package hypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"

	"github.com/hypervisor-io/cloud-controller-manager/pkg/api"
)

var splitAnnKey = AnnotationPrefix + "traffic-split"

const testSplitValue = `[{"service":"web-blue","weight":100},{"service":"web-green","weight":0}]`

// splitPatchRecord is one PATCH the fake master received: the URL path,
// the raw request body (to assert JSON encoding details like [] vs null)
// and the decoded request.
type splitPatchRecord struct {
	Path string
	Raw  string
	Req  api.TrafficSplitRequest
}

// fakeSplitMaster records traffic-split PATCH bodies and answers
// GET /lb/service?uid=... with a running LB (handler shape copied from
// hypervisor_loadbalancer_test.go).
type fakeSplitMaster struct {
	mu      sync.Mutex
	patches []splitPatchRecord
}

func newFakeSplitMaster() (*httptest.Server, *fakeSplitMaster) {
	m := &fakeSplitMaster{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cluster/cid-1/lb/service":
			json.NewEncoder(w).Encode(api.LoadBalancer{
				ID:               "lb-1",
				Status:           "running",
				ServiceUID:       "uid-web",
				ServiceNamespace: "default",
				ServiceName:      "web",
			})
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/traffic-split"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var req api.TrafficSplitRequest
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			m.mu.Lock()
			m.patches = append(m.patches, splitPatchRecord{Path: r.URL.Path, Raw: string(body), Req: req})
			m.mu.Unlock()
			json.NewEncoder(w).Encode(api.TrafficSplitResponse{RuleID: "rule-1"})
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	return srv, m
}

func (m *fakeSplitMaster) snapshot() []splitPatchRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]splitPatchRecord, len(m.patches))
	copy(out, m.patches)
	return out
}

func (m *fakeSplitMaster) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.patches = nil
}

// startTestSplitController builds a trafficSplitController over a fake
// clientset, starts the informer factory and waits for cache sync so
// reconcile() can read Services from the lister.
func startTestSplitController(t *testing.T, masterURL string, objs ...runtime.Object) (*trafficSplitController, *fake.Clientset) {
	t.Helper()
	kc := fake.NewClientset(objs...)
	factory := informers.NewSharedInformerFactory(kc, 0)
	client := &api.Client{
		BaseURL:    masterURL,
		ClusterID:  "cid-1",
		Token:      api.NewFakeTokenLoader("t"),
		HTTPClient: http.DefaultClient,
	}
	ctrl := newTrafficSplitController(client, factory)
	stop := make(chan struct{})
	factory.Start(stop)
	for typ, synced := range factory.WaitForCacheSync(stop) {
		if !synced {
			close(stop)
			t.Fatalf("informer %v did not sync", typ)
		}
	}
	t.Cleanup(func() { close(stop) })
	return ctrl, kc
}

func splitParentSvc(annotations map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "web",
			UID:         types.UID("uid-web"),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
				{Name: "https", Port: 443, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

func splitChildSvc(name string, nodePort int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080, TargetPort: intstr.FromInt(8080), NodePort: nodePort, Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// waitForNoSplitAnnotation blocks until the informer cache reflects the
// removal of the traffic-split annotation from default/web.
func waitForNoSplitAnnotation(t *testing.T, ctrl *trafficSplitController) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		svc, err := ctrl.serviceLister.Services("default").Get("web")
		if err == nil && !hasTrafficSplitAnnotation(svc.Annotations) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("informer cache never observed the annotation removal")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTrafficSplitReconcilePatchesEveryParentPort(t *testing.T) {
	srv, master := newFakeSplitMaster()
	defer srv.Close()

	ctrl, _ := startTestSplitController(t, srv.URL,
		splitParentSvc(map[string]string{splitAnnKey: testSplitValue}),
		splitChildSvc("web-blue", 31080),
		splitChildSvc("web-green", 31090),
	)

	if err := ctrl.reconcile("default/web"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	patches := master.snapshot()
	if len(patches) != 2 {
		t.Fatalf("expected 2 PATCHes (one per parent port), got %d: %+v", len(patches), patches)
	}
	wantEntries := []api.TrafficSplitEntry{
		{RefName: "web-blue", RefNamespace: "default", Weight: 100, NodePort: 31080},
		{RefName: "web-green", RefNamespace: "default", Weight: 0, NodePort: 31090},
	}
	for i, wantPort := range []int{80, 443} {
		got := patches[i]
		if want := "/cluster/cid-1/lb/service/lb-1/traffic-split"; got.Path != want {
			t.Errorf("PATCH %d: path = %s, want %s", i, got.Path, want)
		}
		if got.Req.FrontendPort != wantPort {
			t.Errorf("PATCH %d: frontend_port = %d, want %d", i, got.Req.FrontendPort, wantPort)
		}
		if got.Req.Match != nil {
			t.Errorf("PATCH %d: match = %+v, want nil", i, got.Req.Match)
		}
		if !reflect.DeepEqual(got.Req.Entries, wantEntries) {
			t.Errorf("PATCH %d: entries = %+v, want %+v", i, got.Req.Entries, wantEntries)
		}
	}
}

func TestTrafficSplitRemovingAnnotationSendsClearingPatch(t *testing.T) {
	srv, master := newFakeSplitMaster()
	defer srv.Close()

	ctrl, kc := startTestSplitController(t, srv.URL,
		splitParentSvc(map[string]string{splitAnnKey: testSplitValue}),
		splitChildSvc("web-blue", 31080),
		splitChildSvc("web-green", 31090),
	)

	// Sync the split once so the controller tracks the parent.
	if err := ctrl.reconcile("default/web"); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if got := len(master.snapshot()); got != 2 {
		t.Fatalf("initial sync: expected 2 PATCHes, got %d", got)
	}
	master.reset()

	// Remove the annotation and wait for the informer cache to catch up.
	if _, err := kc.CoreV1().Services("default").Update(context.TODO(), splitParentSvc(nil), metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update parent: %v", err)
	}
	waitForNoSplitAnnotation(t, ctrl)

	if err := ctrl.reconcile("default/web"); err != nil {
		t.Fatalf("reconcile after annotation removal: %v", err)
	}

	patches := master.snapshot()
	if len(patches) != 2 {
		t.Fatalf("expected one clearing PATCH per parent port (2), got %d: %+v", len(patches), patches)
	}
	for i, wantPort := range []int{80, 443} {
		got := patches[i]
		if got.Req.FrontendPort != wantPort {
			t.Errorf("clearing PATCH %d: frontend_port = %d, want %d", i, got.Req.FrontendPort, wantPort)
		}
		if got.Req.Match != nil {
			t.Errorf("clearing PATCH %d: match = %+v, want nil", i, got.Req.Match)
		}
		if got.Req.Entries == nil || len(got.Req.Entries) != 0 {
			t.Errorf("clearing PATCH %d: entries = %#v, want empty non-nil slice", i, got.Req.Entries)
		}
		if !strings.Contains(got.Raw, `"entries":[]`) {
			t.Errorf("clearing PATCH %d: body %s must encode entries as [] not null", i, got.Raw)
		}
	}
}

func TestTrafficSplitChildWithoutNodePortIsSkipped(t *testing.T) {
	srv, master := newFakeSplitMaster()
	defer srv.Close()

	ctrl, _ := startTestSplitController(t, srv.URL,
		splitParentSvc(map[string]string{
			splitAnnKey: `[{"service":"web-blue","weight":70},{"service":"web-pending","weight":30}]`,
		}),
		splitChildSvc("web-blue", 31080),
		splitChildSvc("web-pending", 0),
	)

	// klog only writes to the SetOutput destinations when toStderr is
	// off; restore both defaults afterwards.
	var logBuf bytes.Buffer
	klog.LogToStderr(false)
	klog.SetOutput(&logBuf)
	t.Cleanup(func() {
		klog.SetOutput(os.Stderr)
		klog.LogToStderr(true)
	})

	if err := ctrl.reconcile("default/web"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	klog.Flush()

	patches := master.snapshot()
	if len(patches) != 2 {
		t.Fatalf("expected 2 PATCHes (one per parent port), got %d: %+v", len(patches), patches)
	}
	wantEntries := []api.TrafficSplitEntry{
		{RefName: "web-blue", RefNamespace: "default", Weight: 70, NodePort: 31080},
	}
	for i, got := range patches {
		if !reflect.DeepEqual(got.Req.Entries, wantEntries) {
			t.Errorf("PATCH %d: entries = %+v, want %+v (unresolvable child must be skipped)", i, got.Req.Entries, wantEntries)
		}
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "web-pending") || !strings.Contains(logged, "no allocated NodePort") {
		t.Errorf("expected a warning about web-pending having no allocated NodePort, got log: %s", logged)
	}
}

func TestTrafficSplitEnqueuesTrackedParentAfterAnnotationRemoval(t *testing.T) {
	srv, _ := newFakeSplitMaster()
	defer srv.Close()

	ctrl, _ := startTestSplitController(t, srv.URL,
		splitParentSvc(map[string]string{splitAnnKey: testSplitValue}),
		splitChildSvc("web-blue", 31080),
		splitChildSvc("web-green", 31090),
	)

	// Sync once so the controller tracks the parent.
	if err := ctrl.reconcile("default/web"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Let informer-seeded events settle, then drain the queue so the
	// only key that can appear afterwards comes from our UpdateFunc call.
	time.Sleep(100 * time.Millisecond)
	for ctrl.queue.Len() > 0 {
		k, shutdown := ctrl.queue.Get()
		if shutdown {
			t.Fatal("queue shut down unexpectedly")
		}
		ctrl.queue.Done(k)
	}

	// The annotation-removal update (new object without the annotation)
	// must still enqueue the previously synced parent so reconcile can
	// emit the clearing PATCH.
	ctrl.enqueueIfRelevant(splitParentSvc(nil))
	if n := ctrl.queue.Len(); n != 1 {
		t.Fatalf("tracked parent without annotation must be enqueued for clearing, queue len = %d", n)
	}
	key, shutdown := ctrl.queue.Get()
	if shutdown {
		t.Fatal("queue shut down unexpectedly")
	}
	if key != "default/web" {
		t.Errorf("enqueued key = %v, want default/web", key)
	}
}

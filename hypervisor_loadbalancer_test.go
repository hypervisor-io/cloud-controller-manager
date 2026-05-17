package hypervisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/hypervisor-io/cloud-controller-manager/pkg/api"
)

func makeService(uid string, annotations map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "web",
			UID:         types.UID(uid),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80, NodePort: 30080, Protocol: corev1.ProtocolTCP}},
		},
	}
}

func TestEnsureLoadBalancer_CreatesViaMasterWhen404(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "POST" {
			posted = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			ip := "1.2.3.4"
			json.NewEncoder(w).Encode(api.LoadBalancer{ID: "lb-1", Status: "provisioning", ServiceUID: "uid-1", PublicIP: &ip})
			return
		}
	}))
	defer srv.Close()

	client := &api.Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: api.NewFakeTokenLoader("t"), HTTPClient: http.DefaultClient}
	lb := newLoadBalancer(client)
	svc := makeService("uid-1", map[string]string{"service.beta.kubernetes.io/hypervisor-loadbalancer-plan": "std-1g"})

	_, err := lb.EnsureLoadBalancer(context.TODO(), "test-cluster", svc, []*corev1.Node{})
	if err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Errorf("expected POST when 404")
	}
}

func TestEnsureLoadBalancer_ReturnsStatusWithIPOnceRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := "5.6.7.8"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.LoadBalancer{ID: "lb-1", Status: "running", ServiceUID: "uid-2", PublicIP: &ip})
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: api.NewFakeTokenLoader("t"), HTTPClient: http.DefaultClient}
	lb := newLoadBalancer(client)
	svc := makeService("uid-2", map[string]string{"service.beta.kubernetes.io/hypervisor-loadbalancer-plan": "std-1g"})
	status, err := lb.EnsureLoadBalancer(context.TODO(), "test-cluster", svc, []*corev1.Node{})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Ingress) == 0 || status.Ingress[0].IP != "5.6.7.8" {
		t.Errorf("expected IP 5.6.7.8 in status, got %+v", status)
	}
}

func TestEnsureLoadBalancer_MissingPlanAnnotationReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(api.AnnotationError{Code: "missing_annotation", Field: "plan"})
			return
		}
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: api.NewFakeTokenLoader("t"), HTTPClient: http.DefaultClient}
	lb := newLoadBalancer(client)
	svc := makeService("uid-3", map[string]string{}) // no annotation
	_, err := lb.EnsureLoadBalancer(context.TODO(), "test-cluster", svc, []*corev1.Node{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing_annotation") {
		t.Errorf("expected error mentioning missing_annotation, got %v", err)
	}
}

func TestEnsureLoadBalancerDeleted_404TreatedAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(404)
			return
		}
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: api.NewFakeTokenLoader("t"), HTTPClient: http.DefaultClient}
	lb := newLoadBalancer(client)
	svc := makeService("uid-d", map[string]string{})
	if err := lb.EnsureLoadBalancerDeleted(context.TODO(), "test-cluster", svc); err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

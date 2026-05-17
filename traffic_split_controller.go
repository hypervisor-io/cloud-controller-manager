package hypervisor

import (
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/hypervisor-io/cloud-controller-manager/pkg/annotation"
	"github.com/hypervisor-io/cloud-controller-manager/pkg/api"
)

// trafficSplitController watches LoadBalancer-type Services that carry
// `traffic-split` (or `routing-rules` with `backends`) annotations and
// PATCHes the master with the resolved child NodePorts whenever the
// EndpointSlice topology changes underneath them.
//
// We do NOT replace the upstream service-controller; it owns the
// LB CRUD path (Ensure/Update/Delete). This controller only fans out
// the auxiliary traffic-split PATCH, which the upstream controller
// has no reason to call.
type trafficSplitController struct {
	client *api.Client

	serviceLister corelisters.ServiceLister
	servicesSync  cache.InformerSynced
	slicesSync    cache.InformerSynced

	queue workqueue.RateLimitingInterface

	// childToParents maps "ns/name" of a referenced child Service to the
	// set of parent Service keys ("ns/name") that point at it. Rebuilt
	// for each parent we reconcile. Protected by mu.
	mu             sync.Mutex
	childToParents map[string]map[string]struct{}
}

func newTrafficSplitController(client *api.Client, factory informers.SharedInformerFactory) *trafficSplitController {
	svcInf := factory.Core().V1().Services()
	sliceInf := factory.Discovery().V1().EndpointSlices()

	c := &trafficSplitController{
		client:         client,
		serviceLister:  svcInf.Lister(),
		servicesSync:   svcInf.Informer().HasSynced,
		slicesSync:     sliceInf.Informer().HasSynced,
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "traffic-split"),
		childToParents: map[string]map[string]struct{}{},
	}

	// Enqueue any Service that's type=LoadBalancer with an annotation
	// we care about. The upstream service-controller has already created
	// the parent LB by the time we run; we just push the split overlay.
	svcInf.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { c.enqueueIfRelevant(obj) },
		UpdateFunc: func(_, obj any) { c.enqueueIfRelevant(obj) },
		// On parent delete, master cascades via DeleteLoadBalancer in the
		// upstream service-controller. Nothing to do here.
	})

	// On EndpointSlice change, fan out to every parent Service that
	// references the slice's owner Service.
	sliceInf.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { c.enqueueParentsForSlice(obj) },
		UpdateFunc: func(_, obj any) { c.enqueueParentsForSlice(obj) },
		DeleteFunc: func(obj any) { c.enqueueParentsForSlice(obj) },
	})

	return c
}

// Run blocks until stopCh closes. Caller is responsible for starting the
// shared informer factory before invoking Run.
func (c *trafficSplitController) Run(stopCh <-chan struct{}, workers int) {
	defer c.queue.ShutDown()

	klog.Info("traffic-split controller starting")
	if !cache.WaitForCacheSync(stopCh, c.servicesSync, c.slicesSync) {
		klog.Error("traffic-split controller: caches did not sync")
		return
	}
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	<-stopCh
	klog.Info("traffic-split controller stopping")
}

func (c *trafficSplitController) runWorker() {
	for c.processNext() {
	}
}

func (c *trafficSplitController) processNext() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)
	if err := c.reconcile(key.(string)); err != nil {
		klog.ErrorS(err, "traffic-split reconcile failed", "key", key)
		c.queue.AddRateLimited(key)
		return true
	}
	c.queue.Forget(key)
	return true
}

func (c *trafficSplitController) enqueueIfRelevant(obj any) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return
	}
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return
	}
	if !hasTrafficSplitAnnotation(svc.Annotations) {
		return
	}
	key := svc.Namespace + "/" + svc.Name
	c.queue.Add(key)
}

func (c *trafficSplitController) enqueueParentsForSlice(obj any) {
	slice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		// On final-state-unknown tombstones we lose the slice; ignore —
		// the next periodic resync will pick the parent up.
		return
	}
	ownerSvc := slice.Labels[discoveryv1.LabelServiceName]
	if ownerSvc == "" {
		return
	}
	childKey := slice.Namespace + "/" + ownerSvc

	c.mu.Lock()
	parents := c.childToParents[childKey]
	c.mu.Unlock()

	for parentKey := range parents {
		c.queue.Add(parentKey)
	}
}

func hasTrafficSplitAnnotation(ann map[string]string) bool {
	if ann == nil {
		return false
	}
	if _, ok := ann[AnnotationPrefix+"traffic-split"]; ok {
		return true
	}
	// routing-rules with `backends` is also a split, but checking the
	// raw string for the substring is cheap and avoids a JSON parse
	// just to decide whether to enqueue.
	if v, ok := ann[AnnotationPrefix+"routing-rules"]; ok {
		// Quick reject: only enqueue if the JSON could plausibly contain
		// a backends key. The reconciler does the authoritative parse.
		for i := 0; i+9 <= len(v); i++ {
			if v[i:i+9] == `"backends"` || v[i:i+9] == "'backends" {
				return true
			}
		}
	}
	return false
}

func (c *trafficSplitController) reconcile(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	parent, err := c.serviceLister.Services(ns).Get(name)
	if err != nil {
		// Service vanished; master will cascade via the normal LB delete
		// path. Just clear our child->parent index entries.
		c.dropParent(key)
		return nil
	}
	if parent.Spec.Type != corev1.ServiceTypeLoadBalancer {
		c.dropParent(key)
		return nil
	}

	// Decide whether master even has an LB for us yet. If not, the
	// upstream service-controller will (or has) created one; we re-run
	// when its annotation/status update bounces back through our
	// Service informer.
	lb, err := c.client.GetLoadBalancer(string(parent.UID))
	if err != nil {
		return fmt.Errorf("get LB for %s: %w", key, err)
	}
	if lb == nil {
		// Not yet provisioned. Requeue with backoff (rate-limited add).
		return fmt.Errorf("LB not yet provisioned for %s", key)
	}

	// Parse the standalone split annotation (catch-all) and any
	// routing-rules with `backends` (per-rule splits).
	rawSplit := parent.Annotations[AnnotationPrefix+"traffic-split"]
	standalone, err := annotation.ParseTrafficSplit(rawSplit)
	if err != nil {
		return fmt.Errorf("parse traffic-split: %w", err)
	}

	rawRules := parent.Annotations[AnnotationPrefix+"routing-rules"]
	ruleSpecs, err := annotation.ParseRoutingRuleBackends(rawRules)
	if err != nil {
		return fmt.Errorf("parse routing-rules backends: %w", err)
	}

	// Refresh child->parent index for this parent. We rebuild from
	// scratch so removed refs naturally drop out.
	c.rebuildIndexForParent(key, parent.Namespace, standalone, ruleSpecs)

	if len(standalone) == 0 && len(ruleSpecs) == 0 {
		// No split declared (annotations may have been removed). Master
		// will treat an empty PATCH as "drop CCM-managed splits", but
		// we only emit that PATCH when there's reason to — i.e. once.
		// Skip otherwise to avoid spamming master.
		return nil
	}

	// Resolve children + emit PATCHes.
	if len(standalone) > 0 {
		// Standalone split applies to every parent port — one PATCH per
		// port, match=nil (catch-all).
		entries, warn := c.resolveChildren(parent, standalone)
		for _, w := range warn {
			klog.Warning(w)
		}
		if len(entries) == 0 {
			klog.Warningf("traffic-split %s: no resolvable children, skipping", key)
		} else {
			for _, p := range parent.Spec.Ports {
				req := api.TrafficSplitRequest{
					Entries:      entries,
					FrontendPort: int(p.Port),
					Match:        nil,
				}
				if _, err := c.client.SyncTrafficSplit(lb.ID, req); err != nil {
					return fmt.Errorf("sync split %s port %d: %w", key, p.Port, err)
				}
			}
		}
	}

	// Per-routing-rule splits: one PATCH per (frontend_port, match).
	for _, spec := range ruleSpecs {
		entries, warn := c.resolveChildren(parent, spec.Children)
		for _, w := range warn {
			klog.Warning(w)
		}
		if len(entries) == 0 {
			continue
		}
		var match *api.MatchSpec
		if spec.Match != nil {
			match = &api.MatchSpec{Type: spec.Match.Type, Value: spec.Match.Value}
		}
		req := api.TrafficSplitRequest{
			Entries:      entries,
			FrontendPort: spec.FrontendPort,
			Match:        match,
		}
		if _, err := c.client.SyncTrafficSplit(lb.ID, req); err != nil {
			return fmt.Errorf("sync split %s rule %v: %w", key, spec.Match, err)
		}
	}

	return nil
}

// resolveChildren turns the annotation refs into API entries by looking
// up NodePort + HealthCheckNodePort on the child Services. Returns the
// resolvable subset (skipping refs that can't be looked up) plus a list
// of human warning strings to log.
func (c *trafficSplitController) resolveChildren(parent *corev1.Service, refs []annotation.TrafficSplitChild) ([]api.TrafficSplitEntry, []string) {
	var (
		out      []api.TrafficSplitEntry
		warnings []string
	)
	for _, ref := range refs {
		ns := ref.Namespace
		if ns == "" {
			ns = parent.Namespace
		}
		if ns != parent.Namespace {
			// v1: master enforces same-namespace policy. Warn here so
			// operators see the mismatch in CCM logs even when master
			// rejects.
			warnings = append(warnings, fmt.Sprintf("traffic-split: cross-namespace ref %s/%s under parent %s/%s — master may reject", ns, ref.Service, parent.Namespace, parent.Name))
		}
		child, err := c.serviceLister.Services(ns).Get(ref.Service)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("traffic-split: child %s/%s not found: %v", ns, ref.Service, err))
			continue
		}
		nodePort, hcPort, ok := pickChildNodePort(parent, child)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("traffic-split: child %s/%s has no allocated NodePort, skipping", ns, ref.Service))
			continue
		}
		out = append(out, api.TrafficSplitEntry{
			RefName:             child.Name,
			RefNamespace:        child.Namespace,
			Weight:              ref.Weight,
			NodePort:            nodePort,
			HealthCheckNodePort: hcPort,
		})
	}
	return out, warnings
}

// pickChildNodePort selects which child Service port to use. Preference:
//  1. child port whose `port` equals the parent's `targetPort` for any
//     parent port (lets multi-port LBs map cleanly).
//  2. first port with a non-zero NodePort.
//
// Returns (nodePort, healthCheckNodePort, ok). healthCheckNodePort is
// the child's spec.healthCheckNodePort (only set when child's ETP=Local,
// which is the right value to use when the PARENT is ETP=Local too).
func pickChildNodePort(parent, child *corev1.Service) (int, int, bool) {
	if len(child.Spec.Ports) == 0 {
		return 0, 0, false
	}
	hc := int(child.Spec.HealthCheckNodePort)

	// Build set of parent targetPorts (as ints; named targetPorts are
	// not resolvable here without inspecting child pods).
	parentTargets := map[int]struct{}{}
	for _, pp := range parent.Spec.Ports {
		if pp.TargetPort.Type == intstr.Int {
			parentTargets[pp.TargetPort.IntValue()] = struct{}{}
		}
	}

	// Pass 1: prefer matching port.
	for _, cp := range child.Spec.Ports {
		if cp.NodePort == 0 {
			continue
		}
		if _, hit := parentTargets[int(cp.Port)]; hit {
			return int(cp.NodePort), hc, true
		}
	}
	// Pass 2: fallback first port with a NodePort.
	for _, cp := range child.Spec.Ports {
		if cp.NodePort != 0 {
			return int(cp.NodePort), hc, true
		}
	}
	return 0, 0, false
}

func (c *trafficSplitController) rebuildIndexForParent(parentKey, parentNS string, standalone []annotation.TrafficSplitChild, rules []annotation.TrafficSplitSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Drop existing entries pointing at this parent first.
	for child, parents := range c.childToParents {
		delete(parents, parentKey)
		if len(parents) == 0 {
			delete(c.childToParents, child)
		}
	}

	add := func(refs []annotation.TrafficSplitChild) {
		for _, r := range refs {
			ns := r.Namespace
			if ns == "" {
				ns = parentNS
			}
			child := ns + "/" + r.Service
			set, ok := c.childToParents[child]
			if !ok {
				set = map[string]struct{}{}
				c.childToParents[child] = set
			}
			set[parentKey] = struct{}{}
		}
	}
	add(standalone)
	for _, s := range rules {
		add(s.Children)
	}
}

func (c *trafficSplitController) dropParent(parentKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for child, parents := range c.childToParents {
		delete(parents, parentKey)
		if len(parents) == 0 {
			delete(c.childToParents, child)
		}
	}
}

// runTrafficSplitController boots a SharedInformerFactory around the
// given kubernetes.Interface and runs the controller until stopCh closes.
// This is invoked from hypervisor.Initialize where the upstream framework
// hands us a ControllerClientBuilder.
func runTrafficSplitController(client *api.Client, kc kubernetes.Interface, stopCh <-chan struct{}) {
	factory := informers.NewSharedInformerFactory(kc, 5*time.Minute)
	ctrl := newTrafficSplitController(client, factory)
	factory.Start(stopCh)
	go ctrl.Run(stopCh, 2)
}

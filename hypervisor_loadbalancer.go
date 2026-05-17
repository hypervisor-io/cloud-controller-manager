package hypervisor

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"

	"github.com/hypervisor-io/cloud-controller-manager/pkg/api"
)

type loadBalancer struct {
	client *api.Client
}

func newLoadBalancer(c *api.Client) *loadBalancer {
	return &loadBalancer{client: c}
}

func (lb *loadBalancer) GetLoadBalancer(_ context.Context, _ string, svc *corev1.Service) (*corev1.LoadBalancerStatus, bool, error) {
	found, err := lb.client.GetLoadBalancer(string(svc.UID))
	if err != nil {
		return nil, false, err
	}
	if found == nil {
		return nil, false, nil
	}
	return statusFromLB(found), true, nil
}

func (lb *loadBalancer) GetLoadBalancerName(_ context.Context, _ string, svc *corev1.Service) string {
	return cloudprovider.DefaultLoadBalancerName(svc)
}

func (lb *loadBalancer) EnsureLoadBalancer(_ context.Context, _ string, svc *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	existing, err := lb.client.GetLoadBalancer(string(svc.UID))
	if err != nil {
		return nil, err
	}

	nodeIDs := nodeProviderIDs(nodes)
	ports := svcPortsToAPI(svc.Spec.Ports)
	etp := string(svc.Spec.ExternalTrafficPolicy)
	hcNodePort := int(svc.Spec.HealthCheckNodePort)

	if existing == nil {
		created, err := lb.client.CreateLoadBalancer(api.ServiceCreateRequest{
			ServiceUID:            string(svc.UID),
			ServiceNamespace:      svc.Namespace,
			ServiceName:           svc.Name,
			Annotations:           svc.Annotations,
			Ports:                 ports,
			NodeProviderIDs:       nodeIDs,
			ExternalTrafficPolicy: etp,
			HealthCheckNodePort:   hcNodePort,
		})
		if err != nil {
			return nil, err
		}
		return statusFromLB(created), nil
	}

	// Existing LB — always push ports/annotations/node IDs to master.
	// Kubernetes service-controller invokes Ensure (not Update) when
	// svc.Spec changes, so this is the ONLY path that sees port/annotation
	// diffs. Master's POST `/lb/service` reroutes to updateForService when
	// the LB already exists (idempotency cache + payload-hash dedup
	// collapses no-op repeats), so we don't need to diff client-side.
	if _, err := lb.client.UpdateLoadBalancer(existing.ID, api.ServiceUpdateRequest{
		Ports:                 ports,
		Annotations:           svc.Annotations,
		NodeProviderIDs:       nodeIDs,
		ExternalTrafficPolicy: etp,
		HealthCheckNodePort:   hcNodePort,
	}); err != nil {
		return nil, err
	}
	return statusFromLB(existing), nil
}

func (lb *loadBalancer) UpdateLoadBalancer(_ context.Context, _ string, svc *corev1.Service, nodes []*corev1.Node) error {
	existing, err := lb.client.GetLoadBalancer(string(svc.UID))
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("no LB for service %s", svc.UID)
	}

	desired := nodeProviderIDs(nodes)
	current := hostProviderIDs(existing.Hosts)
	assign, remove := diffSets(desired, current)
	if len(assign)+len(remove) > 0 {
		if _, err := lb.client.SyncHosts(existing.ID, api.SyncHostsRequest{Assign: assign, Remove: remove}); err != nil {
			return err
		}
	}

	// Also update ports if changed.
	if !samePorts(existing.Ports, svcPortsToAPI(svc.Spec.Ports)) {
		if _, err := lb.client.UpdateLoadBalancer(existing.ID, api.ServiceUpdateRequest{Ports: svcPortsToAPI(svc.Spec.Ports), Annotations: svc.Annotations}); err != nil {
			return err
		}
	}
	return nil
}

func (lb *loadBalancer) EnsureLoadBalancerDeleted(_ context.Context, _ string, svc *corev1.Service) error {
	existing, err := lb.client.GetLoadBalancer(string(svc.UID))
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	return lb.client.DeleteLoadBalancer(existing.ID)
}

func statusFromLB(lb *api.LoadBalancer) *corev1.LoadBalancerStatus {
	if lb.PublicIP == nil || *lb.PublicIP == "" {
		return &corev1.LoadBalancerStatus{}
	}
	return &corev1.LoadBalancerStatus{
		Ingress: []corev1.LoadBalancerIngress{{IP: *lb.PublicIP}},
	}
}

func nodeProviderIDs(nodes []*corev1.Node) []string {
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Spec.ProviderID != "" {
			ids = append(ids, n.Spec.ProviderID)
		}
	}
	return ids
}

func hostProviderIDs(hosts []api.Host) []string {
	ids := make([]string, 0, len(hosts))
	for _, h := range hosts {
		ids = append(ids, "hypervisor:///"+h.UUID)
	}
	return ids
}

func diffSets(desired, current []string) (assign, remove []string) {
	cur := map[string]bool{}
	for _, c := range current {
		cur[c] = true
	}
	des := map[string]bool{}
	for _, d := range desired {
		des[d] = true
	}
	for d := range des {
		if !cur[d] {
			assign = append(assign, d)
		}
	}
	for c := range cur {
		if !des[c] {
			remove = append(remove, c)
		}
	}
	return
}

func svcPortsToAPI(ports []corev1.ServicePort) []api.Port {
	out := make([]api.Port, 0, len(ports))
	for _, p := range ports {
		out = append(out, api.Port{Port: int(p.Port), TargetPort: int(p.NodePort), Protocol: string(p.Protocol)})
	}
	return out
}

func samePorts(a, b []api.Port) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

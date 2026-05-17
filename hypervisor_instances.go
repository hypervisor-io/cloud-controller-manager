package hypervisor

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"

	"github.com/hypervisor-io/cloud-controller-manager/pkg/api"
)

type instances struct {
	client *api.Client
}

func newInstances(c *api.Client) *instances {
	return &instances{client: c}
}

func (i *instances) InstanceExists(_ context.Context, node *corev1.Node) (bool, error) {
	md, err := i.client.GetInstance(node.Spec.ProviderID)
	if err != nil {
		return false, err
	}
	return md != nil && md.Exists, nil
}

func (i *instances) InstanceShutdown(_ context.Context, node *corev1.Node) (bool, error) {
	md, err := i.client.GetInstance(node.Spec.ProviderID)
	if err != nil {
		return false, err
	}
	if md == nil {
		return false, nil
	}
	return md.Shutdown, nil
}

func (i *instances) InstanceMetadata(_ context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	md, err := i.client.GetInstance(node.Spec.ProviderID)
	if err != nil {
		return nil, err
	}
	if md == nil {
		return nil, cloudprovider.InstanceNotFound
	}

	addrs := make([]corev1.NodeAddress, 0, len(md.Addresses))
	for _, a := range md.Addresses {
		addrs = append(addrs, corev1.NodeAddress{Type: corev1.NodeAddressType(a.Type), Address: a.Address})
	}
	return &cloudprovider.InstanceMetadata{
		ProviderID:    md.ProviderID,
		InstanceType:  md.InstanceType,
		NodeAddresses: addrs,
		Region:        md.Region,
	}, nil
}

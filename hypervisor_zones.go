package hypervisor

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

type zones struct {
	region string
}

func (z *zones) GetZone(_ context.Context) (cloudprovider.Zone, error) {
	return cloudprovider.Zone{Region: z.region}, nil
}

func (z *zones) GetZoneByProviderID(ctx context.Context, _ string) (cloudprovider.Zone, error) {
	return z.GetZone(ctx)
}

func (z *zones) GetZoneByNodeName(ctx context.Context, _ types.NodeName) (cloudprovider.Zone, error) {
	return z.GetZone(ctx)
}

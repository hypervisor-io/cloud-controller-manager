package hypervisor

import (
	"context"
	"testing"
)

func TestZones_GetZone_ReturnsRegionOnly(t *testing.T) {
	z := &zones{region: "hg-eu-west-1"}
	got, err := z.GetZone(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	if got.Region != "hg-eu-west-1" {
		t.Errorf("got region %q", got.Region)
	}
	if got.FailureDomain != "" {
		t.Errorf("expected zone empty, got %q", got.FailureDomain)
	}
}

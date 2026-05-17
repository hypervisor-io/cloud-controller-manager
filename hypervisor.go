package hypervisor

import (
	"io"
	"net/http"

	"gopkg.in/gcfg.v1"
	cloudprovider "k8s.io/cloud-provider"

	"github.com/hypervisor-io/cloud-controller-manager/pkg/api"
)

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		return newHypervisor(config)
	})
}

type Config struct {
	Global struct {
		APIURL      string `gcfg:"api-url"`
		TokenPath   string `gcfg:"token-path"`
		ClusterID   string `gcfg:"cluster-id"`
		Region      string `gcfg:"region"`
		SslNoVerify bool   `gcfg:"ssl-no-verify"`
	}
}

type hypervisor struct {
	cfg    Config
	client *api.Client
	lb     cloudprovider.LoadBalancer
	inst   cloudprovider.InstancesV2
	zones  cloudprovider.Zones
}

func newHypervisor(r io.Reader) (cloudprovider.Interface, error) {
	var cfg Config
	if err := gcfg.ReadInto(&cfg, r); err != nil {
		return nil, err
	}
	tl, err := api.NewTokenLoader(cfg.Global.TokenPath)
	if err != nil {
		return nil, err
	}
	httpClient := http.DefaultClient
	client := &api.Client{
		BaseURL:    cfg.Global.APIURL,
		ClusterID:  cfg.Global.ClusterID,
		Token:      tl,
		HTTPClient: httpClient,
	}
	h := &hypervisor{cfg: cfg, client: client}
	h.lb = newLoadBalancer(client)
	h.inst = newInstances(client)
	h.zones = &zones{region: cfg.Global.Region}
	return h, nil
}

func (h *hypervisor) Initialize(builder cloudprovider.ControllerClientBuilder, stopCh <-chan struct{}) {
	// Launch the traffic-split controller. It watches LoadBalancer-type
	// Services + EndpointSlices for the parent annotations and PATCHes
	// the platform with resolved child NodePorts. Runs independently of
	// the upstream service-controller's EnsureLoadBalancer loop.
	kc, err := builder.Client("hypervisor-traffic-split")
	if err != nil {
		// Non-fatal: traffic-split is optional. Other controllers still work.
		return
	}
	go runTrafficSplitController(h.client, kc, stopCh)
}
func (h *hypervisor) LoadBalancer() (cloudprovider.LoadBalancer, bool) { return h.lb, true }
func (h *hypervisor) Instances() (cloudprovider.Instances, bool)       { return nil, false }
func (h *hypervisor) InstancesV2() (cloudprovider.InstancesV2, bool)   { return h.inst, true }
func (h *hypervisor) Zones() (cloudprovider.Zones, bool)               { return h.zones, true }
func (h *hypervisor) Clusters() (cloudprovider.Clusters, bool)         { return nil, false }
func (h *hypervisor) Routes() (cloudprovider.Routes, bool)             { return nil, false }
func (h *hypervisor) ProviderName() string                             { return ProviderName }
func (h *hypervisor) HasClusterID() bool                               { return h.cfg.Global.ClusterID != "" }

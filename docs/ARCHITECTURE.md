# Architecture

```
┌────────────────────┐ HTTPS + JWT  ┌──────────────────────────────┐
│ K8s cluster        │ ───────────► │ Hypervisor.io API server     │
│  CCM Pod           │              │   Cluster Controller API     │
│  - hypervisor.go   │              │                              │
│  - hypervisor_lb   │              │     Load Balancer endpoints  │
│  - hypervisor_inst │              │     Instance metadata        │
│  - hypervisor_zone │              │     Region / zone endpoints  │
└────────────────────┘              └──────────────────────────────┘
                                            │
                                            ▼
                                  Hypervisor-managed load balancer
                                  + service correlation by UID
```

## Components

- **`cmd/cloud-controller-manager/main.go`** - entrypoint, registers the
  `hypervisor` cloud provider.
- **`hypervisor.go`** - `cloudprovider.Interface` implementation.
- **`hypervisor_loadbalancer.go`** - `LoadBalancer` interface
  (`EnsureLoadBalancer`, `UpdateLoadBalancer`, `EnsureLoadBalancerDeleted`).
- **`hypervisor_instances.go`** - `InstancesV2` interface (metadata,
  exists, shutdown).
- **`hypervisor_zones.go`** - `Zones` interface (region reporting).
- **`pkg/api/`** - typed REST client; reads JWT from disk and reloads on
  file change (fsnotify).
- **`protocol.go`** - `provider_id` parser and annotation helpers.

## Lifecycle

1. **Service create:** `service-controller` invokes CCM `EnsureLoadBalancer`,
   which POSTs to the API. The platform creates the load balancer
   asynchronously and returns its provisional state.
2. **Polling:** `service-controller` calls `EnsureLoadBalancer` again on
   each reconcile until the load balancer has an external IP. CCM patches
   `Service.status.loadBalancer.ingress` once the IP is known.
3. **Node updates:** Informer notifies CCM of `Node` set changes. CCM
   diffs current vs desired backends and PATCHes the load balancer's
   host list.
4. **Service delete:** `service-controller` calls
   `EnsureLoadBalancerDeleted`, which DELETEs the load balancer.
5. **Token rotation:** Operator re-mints the controller JWT and updates
   the `cluster-controller-token` Secret. `fsnotify` wakes the loader;
   subsequent API calls use the new token transparently.

## Why the API server is the source of truth

- Load balancer lifecycle is owned by the platform; CCM is a *requester*.
- Service UID is stable per K8s Service - used as the correlation key.
- CCM does **not** annotate the Service with a platform-side load
  balancer ID; instead it looks up by UID per call. This makes the
  system robust to lost annotations or replayed Services.

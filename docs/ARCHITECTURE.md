# Architecture

```
┌────────────────────┐ HTTPS+JWT  ┌─────────────────────────────────┐
│ K8s cluster        │ ─────────► │ Master /api/internal/           │
│  CCM Pod           │            │   cluster-controller/v1         │
│  - hypervisor.go   │            │                                 │
│  - hypervisor_lb   │            │ ClusterControllerLoadBalancer   │
│  - hypervisor_inst │            │ Controller (HTTP, JWT+role)     │
│  - hypervisor_zone │            │     │                           │
└────────────────────┘            │     ▼                           │
                                  │ K8sLoadBalancerBridgeService    │
                                  │     ├─► LoadBalancerService     │
                                  │     └─► Instance lookup         │
                                  │                                 │
                                  │ ClusterControllerInstance       │
                                  │ Controller                      │
                                  └─────────────────────────────────┘
                                          │
                                          ▼
                                  load_balancers (existing table)
                                  + service_uid, + kubernetes_cluster_id
```

## Components

- **`cmd/cloud-controller-manager/main.go`** — entrypoint, registers the `external-hypervisor` provider.
- **`hypervisor.go`** — `cloudprovider.Interface` impl.
- **`hypervisor_loadbalancer.go`** — `LoadBalancer` impl (Ensure/Update/Delete).
- **`hypervisor_instances.go`** — `InstancesV2` impl (metadata, exists, shutdown).
- **`hypervisor_zones.go`** — `Zones` impl (region only).
- **`pkg/api/`** — typed REST client; reads JWT from disk and reloads on file change (fsnotify).
- **`protocol.go`** — provider_id parser, annotation helpers.

## Lifecycle

1. **Service create:** kubelet → service-controller → CCM `EnsureLoadBalancer` → `POST /lb/service`. Master creates LB row, dispatches deploy job, returns 201 status=provisioning.
2. **Polling:** service-controller periodically calls `EnsureLoadBalancer` again. Once the LB has a public IP, master returns it. CCM patches `Service.status.loadBalancer.ingress`.
3. **Node updates:** service-controller informer notifies CCM of Node set changes. CCM diffs current vs desired and calls `PATCH /lb/service/{id}/hosts`.
4. **Service delete:** service-controller calls `EnsureLoadBalancerDeleted` → `DELETE /lb/service/{id}`. Master destroys LB.
5. **Token rotation:** Customer re-mints JWT and updates the Secret. fsnotify wakes the loader; subsequent API calls use the new token.

## Why Master is the source of truth

- LB lifecycle is owned by the platform; CCM is a *requester*.
- Service UID is stable per K8s Service — used as the correlation key.
- The CCM does **not** annotate the Service with a master-side LB ID; instead it looks up by UID per call. This makes the system robust to lost annotations or replayed Services.

## Node pools (Slice 11)

The Master gained multi-pool worker support in Slice 11 (one `kubernetes_cluster_node_pools` row per pool, with its own instance plan, scaling bounds, and labels/taints). The CCM is unaffected by this change.

### What CCM reads from Master

CCM only consumes per-instance state through:

- `GET /cluster/{c}/instance?provider_id=hypervisor:///<uuid>` → `InstanceMetadata` for Node label population
- `POST /cluster/{c}/lb/service` and related LB endpoints → keyed by Service UID and the Node's raw `provider_id`

It has never read cluster-level `worker_instance_plan_id`, `worker_count`, `worker_min_size`, or `worker_max_size`. The new per-pool data lives behind the Master endpoints — when CCM asks for a node's metadata, the Master resolves the pool via `kubernetes_cluster_vm_refs.pool_id` server-side and returns the same `InstanceMetadata` shape as before.

### Provider-id shape

The Master uses two provider-id shapes internally:

- **Kubelet provider id** (what ends up on `Node.spec.providerID`, written by cloud-init at worker join time): the legacy plain form `hypervisor:///<instance_uuid>`.
- **CAS autoscaler provider id** (used between Master and the cluster-autoscaler plugin only): the multi-pool form `hypervisor:///pool-<pool_uuid>/<instance_uuid>`.

Because the kubelet receives the legacy form, every `Node.spec.providerID` that the CCM observes is legacy. The CCM's existing `protocol.go` parser and its `nodeProviderIDs` / `hostProviderIDs` round-tripping continue to match, and Master's `K8sLoadBalancerBridgeService::parseProviderId` continues to accept those values. No CCM-side code change is needed for Slice 11.

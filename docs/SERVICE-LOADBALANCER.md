# Service-type=LoadBalancer

How the cloud-controller-manager (CCM) turns a Kubernetes `Service` of
`type: LoadBalancer` into a real HAProxy load balancer VM on the hypervisor
fabric — including SSL, plan selection, and how traffic reaches pods.

The flow shape mirrors AWS/GCP CCMs: `kubectl apply` a Service, the
controller plane reconciles, `.status.loadBalancer.ingress[]` gets a real IP
a few seconds later. The difference is the LB is a managed VM on the same
hypervisor cluster running the workload.

---

## Annotation prefix

Configurable. The CCM and master both read the prefix from
`config('kubernetes.annotation_prefix')`, set via `K8S_ANNOTATION_PREFIX` in
`.env`. Default is the vendor-neutral:

```
service.beta.kubernetes.io/managed-loadbalancer-
```

White-label deployments may override:

```
# .env
K8S_ANNOTATION_PREFIX=service.beta.kubernetes.io/acme-loadbalancer-
```

All annotation keys below are shown with the default prefix. Substitute as
needed.

---

## Annotations

### Required

| Annotation                                                            | Type   | Notes                                                          |
|-----------------------------------------------------------------------|--------|----------------------------------------------------------------|
| `service.beta.kubernetes.io/managed-loadbalancer-plan`                | string | LbPlan name (must be enabled + reachable from cluster's HG)    |

If the annotation is missing or the plan name is unknown/unreachable, the
CCM returns an error to the controller-runtime and the Service stays in
`Provisioning`.

### SSL (optional)

Three modes, picked by `ssl-mode`:

```yaml
# Mode 1: no SSL frontend (default)
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: none
```

```yaml
# Mode 2: Let's Encrypt — master auto-issues + renews
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: api.example.com
```

```yaml
# Mode 3: inline cert + key (base64-encoded PEM)
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: inline
    service.beta.kubernetes.io/managed-loadbalancer-ssl-cert: |
      <base64 PEM fullchain>
    service.beta.kubernetes.io/managed-loadbalancer-ssl-key: |
      <base64 PEM private key>
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: api.example.com   # optional label
```

For Let's Encrypt:
- `ssl-domain` is required
- The domain's A/AAAA must resolve to the LB's public IP before issuance
  succeeds; master polls DNS (`checkPendingDns` cron) and triggers issuance
  once propagation completes
- Renewal runs automatically via the `lb:renew-certificates` cron

For inline:
- The PEMs are stored encrypted in `lb_certificates.certificate` /
  `private_key` (Laravel `encrypted` cast)
- No DNS check — the cert is just installed verbatim

A `kubernetes.io/tls` Secret mode (CCM fetches the Secret and forwards the
bytes as inline) is on the roadmap — not yet wired. For now, encode the
PEM manually with `base64 -w0`.

### Backend protocol (L4 vs L7)

K8s `spec.ports[].protocol` only accepts `TCP`/`UDP`/`SCTP` — there is no
HTTP/HTTPS at the Service level. For L7 features (SSL termination at the
LB, HTTP health checks, header rewrites) HAProxy must run in `mode http`.
Pick that via annotation, AWS-style:

| Annotation | Values | Default | Effect |
|---|---|---|---|
| `backend-protocol` | `tcp`, `http`, `https` | `tcp` | HAProxy frontend + backend `mode`. `http`/`https` enable L7 and pair with the SSL annotations. |

```yaml
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: api.example.com
spec:
  type: LoadBalancer
  ports:
  - port: 443
    targetPort: 80            # pod plain HTTP; LB terminates TLS
```

`tcp` (default) keeps the LB as a transparent L4 passthrough — pods see
the original TLS bytes if any. Use `http` whenever the LB itself
terminates TLS or you want HTTP-aware health checks.

### Other annotations (already supported by `annotationBool` / `annotationCsv` helpers but not yet plumbed end-to-end)

| Annotation                       | Effect                                      |
|----------------------------------|---------------------------------------------|
| `*-subnet`                       | `public` or `private` — picks the IP pool   |
| `*-static-ip`                    | Claim a specific reserved IP                |
| `*-allow-cidrs`                  | CSV of CIDRs for ingress allow rule         |

Add these to the bridge service as the integration grows. The helper API
is in place; only the consumption sites need expanding.

---

## End-to-end lifecycle

### 1. User submits a Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: medium
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: web.example.com
spec:
  type: LoadBalancer
  selector:
    app: web
  ports:
    - port: 443
      targetPort: 8080
      protocol: TCP
```

K8s allocates a NodePort (default range 30000-32767) in addition to the
ClusterIP. The CCM uses that NodePort as its backend port (more on this in
[§ How traffic actually reaches pods](#how-traffic-actually-reaches-pods)).

### 2. CCM reconciles

```
                                                              ┌─────────────────┐
  kubectl apply -f svc.yaml ──┐                               │ master          │
                              ▼                               │  (Laravel)      │
                       ┌──────────────┐   POST /lb            │                 │
                       │ kube-apiserver│ ───────────────────► │ K8sLoadBalancer │
                       └──────────────┘                       │ BridgeService   │
                              ▲                               │       │         │
                              │ updates                       │       ▼         │
                       Service.status.loadBalancer.ingress   │ LoadBalancer-   │
                              │                               │ Service (deploy)│
                              │                               └────────┬────────┘
                              │                                        │ sendCommand
                       ┌──────────────┐                                ▼
                       │ CCM Pod      │                       ┌─────────────────┐
                       │ (in cluster) │                       │ slave hypervisor│
                       │              │   reconcile services  │  spawns LB VM   │
                       │ pkg/cloud/   │ ◄──────────────────── │  + HAProxy cfg  │
                       │ hypervisor   │                       └─────────────────┘
                       └──────────────┘
```

`pkg/cloud/hypervisor` implements `cloudprovider.LoadBalancer`:

| K8s callback                  | CCM method                | Net effect                                    |
|-------------------------------|---------------------------|-----------------------------------------------|
| `EnsureLoadBalancer`          | `EnsureLoadBalancer`      | POST → master `createForService`              |
| `UpdateLoadBalancer`          | `UpdateLoadBalancer`      | PATCH → master `updateForService` + backends  |
| `EnsureLoadBalancerDeleted`   | `EnsureLoadBalancerDeleted` | DELETE → master `destroyForService`         |
| `GetLoadBalancer`             | `GetLoadBalancer`         | read-only status                              |

### 3. Master provisions

`K8sLoadBalancerBridgeService::createForService()`:

1. **Resolve LB plan** — annotation `*-plan` → `LbPlan` row, walks
   `hypervisor_group → lb_plan_groups → plans` to confirm reachability.
2. **Create LB record + backing VM** via
   `LoadBalancerService::deployForKubernetesService`. Same path as the CP
   LB; tagged with `kubernetes_cluster_id` + `service_uid`.
3. **Apply SSL annotations** (this step is non-fatal — if SSL fails the LB
   still comes up; CCM reconciliation retries).
4. **Seed HAProxy frontends + backends** from `spec.ports` + the current
   node provider-IDs.
5. **Return LB public IP** → CCM writes to
   `Service.status.loadBalancer.ingress[0].ip`.

### 4. Updates

When `spec.ports` change, or workers come/go (autoscaler, drain, etc.):

- `samePorts()` short-circuits if frontends didn't change
- `diffSets(desired, current)` computes a minimal assign/remove pair
- HAProxy is reloaded (no restart — zero-downtime)
- SSL annotations are re-applied if changed (`applySslAnnotations` is
  idempotent — active certs are not re-issued)

### 5. Deletion

`destroyForService()` — bills remaining hours, destroys the backing
Instance, frees the public IP, soft-deletes the `LoadBalancer` row.

For full-cluster delete, `cascadeDestroyForCluster()` is called from
`ClusterDelete` so orphaned Service LBs are reaped along with the CP LB.

---

## How traffic actually reaches pods

The LB **does not proxy directly to pod IPs**. Pod CIDR (e.g.
`10.244.0.0/16`) is internal to the cluster overlay (Cilium VXLAN) and is
deliberately not reachable from the LB VM. Traffic flows through the
standard k8s NodePort indirection:

```
External client                  Internet / Public IP
    │
    ▼
LB:443 (HAProxy frontend)
    │
    │   TCP DNAT — round-robin against backend pool
    ▼
[ worker1:30080, worker2:30080, worker3:30080 ]    ← worker VPC IP : Service NodePort
    │
    ▼
Worker NIC receives at 172.16.4.X:30080
    │
    │   Cilium kubeProxyReplacement (eBPF DNAT)
    │   replaces kube-proxy on every worker
    ▼
Pod 10.244.X.Y:8080
```

Key points:

- **NodePort is auto-allocated** by k8s when `spec.type=LoadBalancer`. You
  can pin it with `spec.ports[].nodePort: 31234` if needed; otherwise the
  CCM reads whichever port the apiserver allocated.
- **Backend pool = worker node VPC IPs**, not pod IPs. Workers can come
  and go without HAProxy ever needing to know about individual pods.
- **kube-proxy is replaced by Cilium** (`kubeProxyReplacement: true` in
  cilium-values.yaml). The NodePort DNAT happens in eBPF on the worker.
- **Pod CIDR never leaves the cluster**. Pods reach external services via
  egress NAT (NAT GW or the public path). External clients reach pods
  only via this LB → NodePort → eBPF chain.
- **Pod-to-pod across workers** uses VXLAN overlay on the cluster's VPC.
  None of that is the LB's concern.

This is identical to AWS NLB → EKS NodePort or GCP LB → GKE NodePort.

---

## What lives where in the DB

```
load_balancers
├── id
├── kubernetes_cluster_id   ← non-null for CCM-managed LBs
├── service_uid             ← k8s metadata.uid of the Service
├── service_namespace
├── service_name
├── instance_id             ← backing VM
├── metadata                ← JSON {ports, hosts, annotations}
└── ... (same as platform LBs)
```

`kubernetes_cluster_lb` (pivot) — used **only** for the cluster's
control-plane LB. Service LBs are linked exclusively via
`load_balancers.kubernetes_cluster_id` + `service_uid`. Filtering:

- CP LB: `WHERE EXISTS (SELECT 1 FROM kubernetes_cluster_lb WHERE …)`
- Service LB: `WHERE kubernetes_cluster_id = ? AND service_uid IS NOT NULL`

---

## Failure modes worth knowing

- **`missing_annotation` for `plan`** — Service has no plan annotation. Add
  `<prefix>plan: <name>`.
- **`unknown_lb_plan`** — Plan name doesn't exist or `enabled=0`.
- **`lb_plan_not_in_region`** — Plan exists but the cluster's HG has no
  `LbPlanGroup` linking it. Admin must attach the group.
- **No public IP available** — the atomic claim returns 404; CCM back-offs
  and retries. Watch master logs for `LB deploy failed: ... atomic IP`.
- **LB VM cloud-init dies** (e.g. MTU black-hole) — Service status stays
  empty, CCM stuck. Check `/var/log/cloud-init-output.log` on the LB VM.
- **`missing_annotation: ssl-domain`** — `ssl-mode=letsencrypt` was set but
  `ssl-domain` is empty.
- **LE issuance stuck on `pending_dns`** — Domain's A record doesn't point
  to the LB's public IP. Master's `lb:check-pending-dns` cron retries every
  few minutes.
- **CCM token expired / revoked** — every Service-LB call to master 401s.
  Rotate the `cluster-controller-token` Secret in `kube-system`.

---

## Local development

```
cd cloud-controller-manager
make test            # unit tests, no platform required
```

---

## Operator quick-ref

```bash
# Watch a Service get an IP
kubectl get svc -w

# Inspect the LB UID used as the platform-side correlation key
kubectl get svc web -o jsonpath='{.metadata.uid}'

# Check SSL state via annotations
kubectl get svc web -o jsonpath='{.metadata.annotations}' | jq

# Force re-sync (rare - useful if CCM is wedged)
kubectl annotate svc web service.beta.kubernetes.io/managed-loadbalancer-force-sync=$(date +%s) --overwrite

# Tail CCM logs
kubectl -n kube-system logs deploy/cloud-controller-manager -f
```

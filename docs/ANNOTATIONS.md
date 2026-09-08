# Service Annotations

All annotations use the prefix `service.beta.kubernetes.io/managed-loadbalancer-`.

> The prefix is configurable per master deployment via the
> `K8S_ANNOTATION_PREFIX` `.env` variable. Substitute throughout this
> document if your operator picked a different value.

## Required

| Annotation | Type | Description |
|---|---|---|
| `plan` | string | Name of an LbPlan enabled for the cluster's region. Required - Service creation fails with a Kubernetes Event if missing. |

## Optional

| Annotation | Type | Default | Description |
|---|---|---|---|
| `public` | bool | `true` | Whether the LB has a public IP. |
| `source-ranges` | csv CIDR | `0.0.0.0/0` | Allowed source CIDRs. |
| `proxy-protocol` | `v1`/`v2`/bool | `false` | Send PROXY protocol header to backends. v2 is binary, v1 is text. Backend MUST parse the protocol header or it 502s every request. |
| `ssl-redirect` | bool | `false` | Add HTTP→HTTPS 301 redirect on HTTP-mode frontends (port ≠ 443). Skips ACME challenge path so Let's Encrypt renewals still work. Mirrors AWS `aws-load-balancer-ssl-redirect`. |
| `routing-rules` | JSON | (none) | Array of routing rule objects for SNI/path/host matching. See "Routing rules" below. |
| `traffic-split` | JSON | (none) | Array of weighted child-Service references for blue/green and canary releases. See "Traffic split" below. |
| `idle-timeout` | integer seconds | (HAProxy default: 50 for http/https, 3600 for tcp) | Idle connection timeout applied as `timeout client` on every frontend the Service creates and `timeout server` on the backends they reference. 30..86400; out-of-range values reject the Service with an Event. |
| `connect-timeout` | integer seconds | (HAProxy default: 5) | Time to wait for a connection to a backend pod to establish, applied as `timeout connect` on the backends the Service's frontends reference. 1..75; out-of-range values reject the Service with an Event. |
| `server-timeout` | integer seconds | (falls back to `idle-timeout`) | Maximum time a backend pod has to respond once connected, applied as `timeout server` on the backends the Service's frontends reference - this ONE setting covers both `proxy-read-timeout` and `proxy-send-timeout` from nginx-ingress, since HAProxy has no separate read and send timeout. 1..86400; out-of-range values reject the Service with an Event. |
| `port-{N}-<key>` | * | (falls back to global `<key>`) | Per-port override. Any of: `backend-protocol`, `ssl-mode`, `ssl-domain`, `ssl-cert`, `ssl-key`, `idle-timeout`, `connect-timeout`, `server-timeout`. |

### Per-annotation YAML snippets

```yaml
# public - set false for VPC-internal-only LB (no public IP allocated)
service.beta.kubernetes.io/managed-loadbalancer-public: "false"
```

```yaml
# source-ranges - restrict who can reach the LB at the firewall layer
service.beta.kubernetes.io/managed-loadbalancer-source-ranges: "10.0.0.0/8,1.2.3.4/32"
```

```yaml
# proxy-protocol v2 (binary) - pods need haproxy/nginx proxy-protocol support
service.beta.kubernetes.io/managed-loadbalancer-proxy-protocol: "v2"
```

```yaml
# proxy-protocol v1 (text) - older format; same backend caveat
service.beta.kubernetes.io/managed-loadbalancer-proxy-protocol: "v1"
```

```yaml
# ssl-redirect - auto 301 http://… → https://…
# (requires backend-protocol: http and a port-80 frontend)
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: "http"
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: web.example.com
    service.beta.kubernetes.io/managed-loadbalancer-ssl-redirect: "true"
spec:
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  - name: https
    port: 443
    targetPort: 80
    protocol: TCP
```

```yaml
# public-ip - reserve a specific IP (must be owned by the cluster's user)
service.beta.kubernetes.io/managed-loadbalancer-public-ip: "170.205.54.77"
```

```yaml
# vpc-only - VPC-internal LB, no public IP allocated
service.beta.kubernetes.io/managed-loadbalancer-vpc-only: "true"
```

```yaml
# idle-timeout - keep long-lived websocket / database connections open
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-idle-timeout: "3600"
    service.beta.kubernetes.io/managed-loadbalancer-port-5432-idle-timeout: "86400"
```

```yaml
# connect-timeout / server-timeout - migrating from nginx-ingress's
# proxy-connect-timeout / proxy-read-timeout / proxy-send-timeout. A single
# server-timeout covers both proxy-read-timeout and proxy-send-timeout, since
# HAProxy has no separate read and send timeout.
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-connect-timeout: "10"
    service.beta.kubernetes.io/managed-loadbalancer-server-timeout: "120"
    # Per-port override for a slow upload endpoint on 8443:
    service.beta.kubernetes.io/managed-loadbalancer-port-8443-server-timeout: "600"
```

> There is no `body-size` annotation. nginx-ingress's `proxy-body-size` exists
> to raise its 1 MB default; HAProxy imposes no request-body limit at all, so
> large uploads already work with no setting to look for.
| `public-ip` | string | (auto) | Reserve a specific public IP (must be owned by the cluster's user). |
| `vpc-only` | bool | `false` | If true, no public IP - VPC-internal only. |
| `internal` | bool | `false` | VPC-internal LB, no public IP allocated. Equivalent to `vpc-only: true` or `public: false`; `vpc-only: true` combined with an explicit `public: true` is rejected with an Event. |

## Example

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
  annotations:
    # Plan (required)
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g

    # SSL (optional)
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: web.example.com

    # HTTP → HTTPS auto-redirect (port 80 → 443). Issues a 301. Skips the
    # ACME challenge path so Let's Encrypt renewals on port 80 still work.
    # Backend-protocol must be `http` for this to engage (haproxy needs L7).
    service.beta.kubernetes.io/managed-loadbalancer-ssl-redirect: "true"
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: "http"

    # Security groups (optional)
    service.beta.kubernetes.io/managed-loadbalancer-security-groups: web-allow-443,monitoring-allow-9100

    # Other
    service.beta.kubernetes.io/managed-loadbalancer-public: "true"
    service.beta.kubernetes.io/managed-loadbalancer-source-ranges: "10.0.0.0/8,1.2.3.4/32"
    service.beta.kubernetes.io/managed-loadbalancer-proxy-protocol: "false"
spec:
  type: LoadBalancer
  selector:
    app: nginx
  ports:
  # Expose port 80 too so the redirect target exists. Without an 80
  # frontend on the LB there's nothing to redirect FROM.
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  - name: https
    port: 443
    targetPort: 80
    protocol: TCP
```

## SSL / TLS termination

The LB can terminate TLS using one of three modes via the `ssl-mode`
annotation. Default is `none`.

| Annotation | Required when | Description |
|---|---|---|
| `ssl-mode` | always (defaults to `none`) | `none`, `letsencrypt`, or `inline`. |
| `ssl-domain` | `letsencrypt` mode (required); `inline` mode (optional label) | FQDN to issue / label. Its A/AAAA must resolve to the LB's public IP for `letsencrypt`. |
| `ssl-cert` | `inline` mode | Base64-encoded PEM (fullchain). |
| `ssl-key`  | `inline` mode | Base64-encoded PEM (private key). |

### Let's Encrypt

```yaml
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: api.example.com
```

The platform watches DNS propagation and triggers Let's Encrypt issuance
once the domain resolves to the load balancer's public IP. Renewal is
automatic.

### Inline cert + key

```yaml
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: inline
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: api.example.com
    service.beta.kubernetes.io/managed-loadbalancer-ssl-cert: |
      <base64 PEM fullchain>
    service.beta.kubernetes.io/managed-loadbalancer-ssl-key: |
      <base64 PEM private key>
```

Encode the PEMs with `base64 -w0 fullchain.pem` and `base64 -w0 privkey.pem`.
PEMs are stored encrypted on master (Laravel `encrypted` cast).

### `kubernetes.io/tls` Secret mode (planned)

A `ssl-mode: secret` + `ssl-secret-name: my-tls` pattern is on the roadmap.
Until shipped, dereference the Secret manually and use the `inline` mode.

## Backend protocol (L4 vs L7)

K8s `spec.ports[].protocol` only accepts `TCP`/`UDP`/`SCTP`. For L7 features
(SSL termination at LB, HTTP health checks, header rewrites) HAProxy must
run in `mode http`. Pick via annotation, AWS-style.

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
    targetPort: 80           # Pod plain HTTP; LB terminates TLS
```

`tcp` (default) keeps the LB as a transparent L4 passthrough - pods see the
original TLS bytes if any. Use `http` whenever the LB itself terminates TLS
or you want HTTP-aware health checks.

## Health checks (active)

HAProxy probes each backend periodically. Defaults are sensible - override
only what you need.

| Annotation | Type | Default | Description |
|---|---|---|---|
| `health-check-enabled` | bool | `true` | Disable to stop active probing entirely. |
| `health-check-protocol` | `tcp` \| `http` | mirrors `backend-protocol` | Probe layer. `http` enables `path` + `expect`. |
| `health-check-port` | int \| `traffic-port` | NodePort | Where to probe. `traffic-port` means the same port as live traffic. |
| `health-check-path` | string | `/` (http) | HTTP path. Ignored for tcp. |
| `health-check-interval` | seconds | `5` | Probe frequency. |
| `health-check-timeout` | seconds | `3` | Per-probe timeout. |
| `health-check-healthy-threshold` | int | `2` | Consecutive OK probes before backend marked UP. |
| `health-check-unhealthy-threshold` | int | `3` | Consecutive failures before marked DOWN. |
| `health-check-expect` | string | (unset) | http only. Example: `status 200` or `string OK`. |

```yaml
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-health-check-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-health-check-path: /healthz
    service.beta.kubernetes.io/managed-loadbalancer-health-check-interval: "5"
    service.beta.kubernetes.io/managed-loadbalancer-health-check-expect: status 200
```

## Passive checks (observe live traffic)

HAProxy can also watch real traffic and mark a server down on repeated
errors - cheaper than active probing alone and catches failures that only
manifest under load.

| Annotation | Type | Default | Description |
|---|---|---|---|
| `passive-check-enabled` | bool | `false` | Turn on `observe` on each server. |
| `passive-check-error-limit` | int | `10` | Consecutive errors before action fires. |
| `passive-check-on-error` | `mark-down` \| `fail-check` \| `sudden-death` \| `fastinter` | `mark-down` | What to do when the limit is hit. |

```yaml
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-passive-check-enabled: "true"
    service.beta.kubernetes.io/managed-loadbalancer-passive-check-error-limit: "5"
    service.beta.kubernetes.io/managed-loadbalancer-passive-check-on-error: mark-down
```

Layer is picked automatically - `layer7` when backend mode is http, else
`layer4`.

## Security groups

Attach one or more existing Security Groups (by name or UUID, scoped to the
cluster owner) to the LB's backing instance.

| Annotation | Type | Description |
|---|---|---|
| `security-groups` | csv | Comma-separated SG names or UUIDs (mixed OK). Unknown entries are skipped with a warning. |

```yaml
metadata:
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-security-groups: web-allow-443,monitoring-allow-9100
```

SGs are re-applied on every Service update so changes propagate without
recreating the LB.

## Per-port settings

Any global annotation can be overridden for a single frontend port using the
pattern `<prefix>port-{N}-<key>`, where `{N}` is the `spec.ports[].port` value
and `<key>` is one of: `backend-protocol`, `ssl-mode`, `ssl-domain`,
`ssl-cert`, `ssl-key`, `idle-timeout`, `connect-timeout`, `server-timeout`. If a per-port key is
unset, the corresponding global annotation is used. If neither is set, the documented default
applies.

This lets a single Service expose plain TCP, Let's Encrypt-terminated HTTPS,
inline-cert HTTPS, and another raw TCP port - all from one LB.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: multi-proto
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g

    # Port 80 - plain HTTP passthrough (default TCP mode, no SSL)
    service.beta.kubernetes.io/managed-loadbalancer-port-80-backend-protocol: tcp

    # Port 443 - HTTP mode + Let's Encrypt for web.example.com
    service.beta.kubernetes.io/managed-loadbalancer-port-443-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-port-443-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-port-443-ssl-domain: web.example.com

    # Port 8443 - HTTP mode + inline cert for api.example.com
    service.beta.kubernetes.io/managed-loadbalancer-port-8443-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-port-8443-ssl-mode: inline
    service.beta.kubernetes.io/managed-loadbalancer-port-8443-ssl-domain: api.example.com
    service.beta.kubernetes.io/managed-loadbalancer-port-8443-ssl-cert: |
      <base64 PEM fullchain>
    service.beta.kubernetes.io/managed-loadbalancer-port-8443-ssl-key: |
      <base64 PEM private key>

    # Port 5432 - raw TCP passthrough to Postgres pods
    service.beta.kubernetes.io/managed-loadbalancer-port-5432-backend-protocol: tcp
spec:
  type: LoadBalancer
  selector:
    app: web
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  - name: https
    port: 443
    targetPort: 80
    protocol: TCP
  - name: api
    port: 8443
    targetPort: 8080
    protocol: TCP
  - name: postgres
    port: 5432
    targetPort: 5432
    protocol: TCP
```

## Routing rules (advanced)

For workloads that need multiple TLS certs on the same port, or that route
by URL path/Host header, set the `routing-rules` annotation to a JSON array.
Each rule binds to one port on the LB and specifies how to match incoming
traffic.

Schema:

```json
[
  {"port": 443, "match": "sni|path|host", "value": "...", "ssl_domain": "..."}
]
```

| Field | Required | Description |
|---|---|---|
| `port` | yes | LB frontend port the rule applies to. Must exist in `spec.ports`. |
| `match` | yes | One of `sni`, `path`, `host`. |
| `value` | yes | Matched value - domain for `sni`/`host`, URL prefix for `path`. |
| `ssl_domain` | sni only | Per-rule Let's Encrypt domain. Issued separately and added to the frontend's crt-list. |

### SNI multi-domain on one port

Most common case - one port 443, multiple TLS certs, HAProxy picks the right
cert from the ClientHello SNI extension.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web-multi-sni
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: web.example.com
    service.beta.kubernetes.io/managed-loadbalancer-routing-rules: |
      [
        {"port": 443, "match": "sni", "value": "web.example.com",   "ssl_domain": "web.example.com"},
        {"port": 443, "match": "sni", "value": "shop.example.com",  "ssl_domain": "shop.example.com"},
        {"port": 443, "match": "sni", "value": "api.example.com",   "ssl_domain": "api.example.com"}
      ]
spec:
  type: LoadBalancer
  selector:
    app: web
  ports:
  - name: https
    port: 443
    targetPort: 80
    protocol: TCP
```

The global `ssl-domain` provides the default cert (served when the client
sends no SNI or sends an unknown hostname). Each rule adds its own cert.

### Path-based routing

Route by URL path prefix - useful for splitting `/api` vs `/` to different
backend pools. No `ssl_domain` needed; TLS termination is governed by the
global `ssl-*` annotations.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web-paths
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: web.example.com
    service.beta.kubernetes.io/managed-loadbalancer-routing-rules: |
      [
        {"port": 443, "match": "path", "value": "/api"},
        {"port": 443, "match": "path", "value": "/static"}
      ]
spec:
  type: LoadBalancer
  ports:
  - name: https
    port: 443
    targetPort: 80
    protocol: TCP
```

### Host-based routing

Route by HTTP `Host:` header (L7, requires `backend-protocol: http`). Unlike
SNI, this works after TLS termination so it can split traffic that arrived
under the same cert.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web-hosts
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-ssl-mode: letsencrypt
    service.beta.kubernetes.io/managed-loadbalancer-ssl-domain: web.example.com
    service.beta.kubernetes.io/managed-loadbalancer-routing-rules: |
      [
        {"port": 443, "match": "host", "value": "admin.example.com"},
        {"port": 443, "match": "host", "value": "www.example.com"}
      ]
spec:
  type: LoadBalancer
  ports:
  - name: https
    port: 443
    targetPort: 80
    protocol: TCP
```

### Multi-cert SNI behind the scenes

When two or more certificates are bound to a single frontend (the default
cert from `ssl-domain` plus one per SNI rule), the LB renders the frontend
with:

```
bind :443 ssl crt-list /etc/haproxy/certs/{frontend_id}.crt-list
```

The slave writes that crt-list file alongside each `{cert_id}.pem`. Each
line in the crt-list is one PEM path optionally followed by an SNI label;
the entry with no label is HAProxy's fallback when the client sends no SNI
(or sends a hostname none of the labelled certs match).

### Reconciliation

`routing-rules` is reconciled to the `LbRoutingRule` table on each Service
update:

- Adding a rule object creates a new `LbRoutingRule`.
- Removing a rule object from the JSON deletes the corresponding
  `LbRoutingRule` (and any Let's Encrypt cert tied to it, if its
  `ssl_domain` is no longer referenced).
- Setting the annotation to `[]` wipes all rules.
- **Unsetting the annotation entirely** (removing the key) leaves existing
  rules alone - useful when transitioning management of routing back to a
  human operator.

## Traffic split (weighted backends)

Split traffic across **multiple child Services** with relative weights. The canonical use case is blue/green and canary deploys: keep `app-blue` Service taking 95% of traffic while `app-green` takes 5%, then shift the weights as confidence grows.

Requires a management server that implements `PATCH /lb/service/{lb_id}/traffic-split` (rebrand/vcli-brand from 2026-09-07 onward); older masters answer 501 and the controller retries with backoff.

Two forms:

| Form | Annotation | Applies to | Match |
|---|---|---|---|
| Standalone | `traffic-split` | every frontend port on the parent Service | none (catch-all) |
| Embedded | `routing-rules[].backends` | one routing rule | the rule's `match`+`value` |

Schema (both forms share the child object):

```json
[
  {"service": "app-blue",  "namespace": "production", "weight": 80},
  {"service": "app-green", "namespace": "production", "weight": 20}
]
```

| Field | Required | Description |
|---|---|---|
| `service` | yes | Child Service name in the same K8s cluster. |
| `namespace` | no | Defaults to the parent Service's namespace. |
| `weight` | yes | Integer 0-1000. **Relative**, not percent - `[80,20]` and `[400,100]` behave identically; HAProxy normalises. `weight: 0` drains a backend without removing it. |

The parent Service still needs a normal `spec.selector` + `ports` block. The selector's pods serve traffic only if the split list also includes the parent Service's own name (otherwise the selector is effectively dead weight - the parent acts purely as the LB anchor). Most deployments leave the parent's selector pointing at one of the split children and never rely on it directly.

### Blue / green (standalone)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-backend-protocol: http
    service.beta.kubernetes.io/managed-loadbalancer-traffic-split: |
      [
        {"service": "web-blue",  "weight": 100},
        {"service": "web-green", "weight": 0}
      ]
spec:
  type: LoadBalancer
  selector:
    app: web
  ports:
  - name: http
    port: 80
    targetPort: 8080
    protocol: TCP
```

Cut over by flipping the weights to `[0, 100]` (one `kubectl apply`). The split applies to **every port** in `spec.ports`.

### Canary (embedded under a routing rule)

Same `routing-rules` schema as above, with a `backends` field on the rule that should split:

```yaml
service.beta.kubernetes.io/managed-loadbalancer-routing-rules: |
  [
    {
      "port": 443,
      "match": "host",
      "value": "admin.example.com",
      "backends": [
        {"service": "admin-v1", "weight": 95},
        {"service": "admin-v2", "weight": 5}
      ]
    },
    {
      "port": 443,
      "match": "host",
      "value": "www.example.com"
    }
  ]
```

`admin.example.com` requests are split 95/5 between `admin-v1` and `admin-v2`. `www.example.com` requests follow the rule's implicit single-backend path (parent Service's selector).

### Why CCM resolves this client-side

The master cannot map a Service name to a NodePort on its own - that lookup needs the K8s informer cache. The CCM (`pkg/annotation/trafficsplit.go`) resolves each `service`+`namespace` to `node_port` + `health_check_node_port` and submits the resolved entries via `PATCH /lb/service/{id}/traffic-split` (`pkg/api/client.go:117-124`, `pkg/api/types.go:70-96`). HAProxy then targets `node:NodePort` directly per child, bypassing kube-proxy's load-balancing layer (so the weights aren't smeared by two layers of LB).

### Reconciliation

`traffic-split` reconciles to the `LbTrafficSplit` + `LbTrafficSplitChild` tables on each Service update:

- Adding a child object inserts a new child row.
- Removing a child removes its row and any HAProxy `server` line tied to it.
- Setting the annotation to `[]` wipes the split (parent's selector resumes serving 100%).
- **Unsetting the annotation entirely** leaves the split intact - same opt-out pattern as `routing-rules`.

If a referenced child Service does not exist (or has no Endpoints), the CCM raises a `CreateLoadBalancerFailed` Event with `unresolved_service` and the offending name. The split is not partially applied - either every child resolves or none do.

## Errors

If an annotation is invalid or missing, `kubectl describe svc web` shows a `Warning  CreateLoadBalancerFailed` Event with the error code (e.g. `missing_annotation`, `unknown_lb_plan`, `lb_plan_not_in_region`, `plan_not_ha_capable`, `invalid_cidr`, `unresolved_service`).

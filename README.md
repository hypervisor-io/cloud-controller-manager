# cloud-controller-manager-hypervisor

Kubernetes Cloud Controller Manager for clusters managed by the Hypervisor
panel. Provides:

- **Node provider** - reconciles `Node` objects against panel-tracked
  instances (zones, instance type, addresses, conditions)
- **Service Load Balancer** - reconciles Services of `type=LoadBalancer`
  against panel-managed load balancers (HAProxy backing), honoring
  annotations for routing rules, SSL, public-IP pinning, security groups
- **Traffic Split controller** - implements blue/green + canary routing
  via `lb.hypervisor.io/traffic-split` annotations

## Quick build

```bash
# Local binary
make build              # → bin/cloud-controller-manager

# Container image
make image push REGISTRY=ghcr.io/<org>
# → ghcr.io/<org>/cloud-controller-manager-hypervisor:<git-describe>
```

`VERSION` defaults to `git describe --tags --always` or `dev` if no tag.
Override explicitly:

```bash
make image push REGISTRY=ghcr.io/<org> VERSION=1.0.0
```

## Prerequisites

- Go 1.22+ (host build) OR Docker (multi-stage container build)
- For `make push`: `docker login` to your target registry

```bash
echo $GHCR_PAT | docker login ghcr.io -u <github-user> --password-stdin
```

## Tests

```bash
make test           # go test ./...
```

Provider has unit tests for instance discovery, zone reporting, load
balancer reconciliation, traffic split annotation parsing, and protocol
message wire format.

## What you get

| Artifact                                          | Where                                                          |
|---------------------------------------------------|----------------------------------------------------------------|
| Container image (distroless, linux/amd64)         | `$REGISTRY/cloud-controller-manager-hypervisor:$VERSION`       |
| Local binary                                      | `./bin/cloud-controller-manager`                               |

## Provider behaviour

- Talks to the Hypervisor panel via the controller-token JWT
  (same auth flow as cluster-autoscaler-hypervisor).
- Service annotations recognized:
  - `lb.hypervisor.io/public-ip` - pin a specific public IP (must be
    pre-owned via the static-IP feature)
  - `lb.hypervisor.io/routing-rules` - SNI/path-based routing rules JSON
  - `lb.hypervisor.io/traffic-split` - weighted multi-backend split
  - `lb.hypervisor.io/health-check-*` - HAProxy health-check overrides
  - Full list in [docs/ANNOTATIONS.md](docs/ANNOTATIONS.md)
- Service of type=LoadBalancer creates a load balancer in the cluster's VPC
  via the panel's LB API; deletion cleans up the backing LB.

## Deploy

The Hypervisor panel auto-deploys CCM when a cluster is created. Manual
deployment steps + reference manifest in
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## Render the deployment manifest

```bash
make manifest \
  CLUSTER_ID=<cluster-uuid> \
  MASTER_URL=https://panel.example.com \
  REGISTRY=ghcr.io/<org> \
  REGION=<region-name>
```

Outputs the rendered manifest to stdout. Pipe to `kubectl apply -f -`.

## More

- [docs/BUILDING.md](docs/BUILDING.md) - full build details
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) - components + their wiring
- [docs/CONFIGURATION.md](docs/CONFIGURATION.md) - flags, cloud-config keys
- [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) - manifest, RBAC, scaling
- [docs/ANNOTATIONS.md](docs/ANNOTATIONS.md) - Service annotations reference
- [docs/SERVICE-LOADBALANCER.md](docs/SERVICE-LOADBALANCER.md) - LB lifecycle details
- [docs/README.md](docs/README.md) - documentation index
so tr
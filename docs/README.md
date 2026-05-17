# cloud-controller-manager

Kubernetes Cloud Controller Manager (CCM) for Hypervisor.io clusters. Implements the upstream `cloudprovider.Interface` to provision LoadBalancers (Service: type=LoadBalancer) and populate Node metadata against the Hypervisor.io platform.

## Capabilities

- `Service: type=LoadBalancer` → provisions a Hypervisor.io LoadBalancer; populates `EXTERNAL-IP` once ready.
- Node provider-id resolution → labels Nodes with region + instance-type.
- BYO install — customer applies CCM manifest + Secret manually.

## Required Service annotations

- `service.beta.kubernetes.io/managed-loadbalancer-plan` — **required**. Name of an LbPlan enabled in the cluster's region. Example: `std-1g`.

See `docs/ANNOTATIONS.md` for full list (public, source-ranges, proxy-protocol, ha, firewall, etc.).

## Quick start

```bash
# 1. Build image
make image push REGISTRY=ghcr.io/<your-org>

# 2. Mint a JWT for the cluster (run on master server)
php artisan kubernetes:mint-token --cluster=<cluster-uuid> --role=ccm > token.txt

# 3. Render manifest
make manifest CLUSTER_ID=<cluster-uuid> MASTER_URL=https://panel.example.com REGION=<region-slug> > rendered.yaml

# 4. Apply to cluster
kubectl apply -f rendered.yaml

# 5. Apply Secret with the token
kubectl create secret generic cluster-controller-token \
  --from-file=token=token.txt -n kube-system

# 6. Verify rollout
kubectl -n kube-system rollout status deploy cloud-controller-manager
```

See `docs/DEPLOYMENT.md` for full step-by-step.

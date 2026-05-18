# Deploying cloud-controller-manager-hypervisor

## Prerequisites

- A running Hypervisor.io Kubernetes cluster (state=running)
- Kubernetes admin access (`kubectl`)
- `cluster-controller-manager-hypervisor` image accessible to your cluster nodes (see `BUILDING.md` for push instructions)
- A controller JWT for the target cluster (see step 2 below)

## Steps

### 1. Mint a controller JWT

Run on the Hypervisor.io master server:

```bash
php artisan kubernetes:mint-token --cluster=<cluster-uuid> --role=ccm --ttl=86400 > token.txt
```

The output is a single JWT line. Save it to `token.txt`.

### 2. Render the manifest

```bash
make manifest \
  CLUSTER_ID=<cluster-uuid> \
  MASTER_URL=https://master.example.com \
  REGION=<region-slug> \
  REGISTRY=ghcr.io/<org> \
  > rendered.yaml
```

`REGION` is the slug of the cluster's HypervisorGroup (visible in the user dashboard).

### 3. Apply to the cluster

```bash
kubectl apply -f rendered.yaml
```

### 4. Create the Secret with the JWT

```bash
kubectl create secret generic cluster-controller-token \
  --from-file=token=token.txt \
  -n kube-system
```

### 5. Verify the deployment

```bash
kubectl -n kube-system rollout status deploy cloud-controller-manager-hypervisor
kubectl -n kube-system logs deploy/cloud-controller-manager-hypervisor -f
```

## Token rotation

Re-mint:

```bash
php artisan kubernetes:mint-token --cluster=<cluster-uuid> --role=ccm > token.txt
kubectl -n kube-system create secret generic cluster-controller-token \
  --from-file=token=token.txt --dry-run=client -o yaml | kubectl apply -f -
```

The CCM watches `/etc/controller/token` via fsnotify and reloads automatically.

## Smoke test

Apply a sample Service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
  annotations:
    service.beta.kubernetes.io/managed-loadbalancer-plan: std-1g
    service.beta.kubernetes.io/managed-loadbalancer-public: "true"
spec:
  type: LoadBalancer
  selector:
    app: nginx
  ports:
  - port: 80
    targetPort: 80
```

Watch:

```bash
kubectl get svc -w
```

`EXTERNAL-IP` should populate within ~2 minutes.

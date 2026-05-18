# Configuration

The CCM reads its config from a gcfg-style INI file (mounted as a ConfigMap volume at `/etc/cloud-config/cloud-config`).

## Cloud-config keys

```ini
[Global]
api-url       = https://master.example.com/api/internal/cluster-controller/v1
token-path    = /etc/controller/token
cluster-id    = 11111111-1111-1111-1111-111111111111
region        = hg-eu-west-1
ssl-no-verify = false
```

| Key | Description |
|---|---|
| `api-url` | Master internal API base URL — `<master>/api/internal/cluster-controller/v1` |
| `token-path` | Path to JWT token file on disk (mounted from Secret) |
| `cluster-id` | UUID of the Hypervisor.io cluster |
| `region` | HypervisorGroup slug — used as Node label `topology.kubernetes.io/region` |
| `ssl-no-verify` | Disable TLS verification (only for testing) |

## CLI flags

The CCM is invoked with standard Kubernetes cloud-provider flags. The manifest template sets:

- `--cloud-provider=external-hypervisor`
- `--cloud-config=/etc/cloud-config/cloud-config`
- `--leader-elect=true`
- `--use-service-account-credentials=true`
- `--controllers=cloud-node,cloud-node-lifecycle,service`
- `--v=2`

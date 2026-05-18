# Building cloud-controller-manager-hypervisor

## Local binary

```bash
make build   # produces ./bin/cloud-controller-manager
```

Requires Go 1.22+.

## Container image

```bash
make image                      # local tag
make image push REGISTRY=ghcr.io/<org>
```

The Dockerfile is multi-stage and produces a distroless final image. The default tag is `$(REGISTRY)/cloud-controller-manager-hypervisor:$(VERSION)` where `VERSION` is `git describe` output (or `dev`).

## Pushing to ghcr.io

1. Authenticate:

```bash
echo $GHCR_PAT | docker login ghcr.io -u <github-user> --password-stdin
```

2. Set REGISTRY to your GHCR namespace and push:

```bash
make image push REGISTRY=ghcr.io/<org>
```

Where `<org>` is your GitHub organization or username. For an org, ensure the package visibility allows your Kubernetes nodes to pull (public package, or imagePullSecret with a PAT).

## Tests

```bash
make test
```

Runs `go test ./...`.

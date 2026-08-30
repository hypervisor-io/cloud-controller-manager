# syntax=docker/dockerfile:1.4
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=$(git describe --tags --always 2>/dev/null || echo dev)" -o /out/cloud-controller-manager ./cmd/cloud-controller-manager

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cloud-controller-manager /usr/local/bin/cloud-controller-manager
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cloud-controller-manager"]

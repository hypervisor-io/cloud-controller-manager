SHELL := /bin/bash

REGISTRY ?= ghcr.io/hypervisor-io
IMAGE_NAME ?= cloud-controller-manager
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
IMAGE := $(REGISTRY)/$(IMAGE_NAME):$(VERSION)

.PHONY: all test build image push manifest fmt lint

all: test build

test:
	go test ./...

build:
	CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -o bin/cloud-controller-manager ./cmd/cloud-controller-manager

image:
	docker build -t $(IMAGE) .
	@echo "Built: $(IMAGE)"

push:
	docker push $(IMAGE)

# Render deploy/manifest-template.yaml with substitutions for direct kubectl apply.
# Usage: make manifest CLUSTER_ID=<uuid> MASTER_URL=https://master.example.com REGISTRY=ghcr.io/<org>
manifest:
	@if [ -z "$(CLUSTER_ID)" ] || [ -z "$(MASTER_URL)" ]; then \
		echo "ERROR: CLUSTER_ID and MASTER_URL are required"; exit 1; \
	fi
	@sed \
		-e 's|{{CLUSTER_ID}}|$(CLUSTER_ID)|g' \
		-e 's|{{MASTER_URL}}|$(MASTER_URL)|g' \
		-e 's|{{IMAGE}}|$(IMAGE)|g' \
		-e 's|{{REGION}}|$(REGION)|g' \
		deploy/manifest-template.yaml

fmt:
	gofmt -w .

lint:
	go vet ./...

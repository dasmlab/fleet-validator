.PHONY: build test lint run once image dashboards render diagrams

VERSION ?= dev
IMG ?= ghcr.io/dasmlab/fleet-validator:$(VERSION)
CONTEXT ?=

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/fleet-validator ./cmd/fleet-validator

test:
	go test ./...

lint:
	golangci-lint run --timeout=5m

## Serve UI + metrics locally against a kubeconfig context: make run CONTEXT=<ctx>
run: build
	./bin/fleet-validator --context=$(CONTEXT)

## One validation pass, JSON to stdout: make once CONTEXT=<ctx>
once: build
	./bin/fleet-validator --context=$(CONTEXT) --once

image:
	podman build --build-arg VERSION=$(VERSION) -t $(IMG) .

## Regenerate dashboards, their ConfigMaps and the hub policy
dashboards:
	python3 hack/gen-dashboards.py
	./hack/render-grafana-configmap.sh

render: dashboards
	./hack/render-acm-policy.sh

diagrams:
	for f in $$(git ls-files '*.d2'); do d2 "$$f" "$${f%.d2}.svg"; done

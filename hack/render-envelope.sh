#!/usr/bin/env bash
# Print the plain-manifest envelope for clusters without ACM (2026-prod-1): the same objects
# the hub policy creates, minus the ACM Observability dashboards.
#   IMAGE=ghcr.io/dasmlab/fleet-validator:v0.1.1-abc1234 ./hack/render-envelope.sh > envelope.yaml
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

: "${IMAGE:?set IMAGE}"
tmp="$(mktemp)"
trap 'rm -f "${tmp}"' EXIT
IMAGE="${IMAGE}" OUT="${tmp}" ./hack/render-acm-policy.sh >/dev/null

go run ./hack/envelope "${tmp}"

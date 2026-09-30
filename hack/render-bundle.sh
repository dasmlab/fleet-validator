#!/usr/bin/env bash
# Print the GitOps bundle Argo CD applies on the hub: the hub policy pinned to IMAGE, its
# Placement, and the spoke probe policy + Placement.
#   IMAGE=ghcr.io/dasmlab/fleet-validator:v0.1.0-abc1234 ./hack/render-bundle.sh > bundle.yaml
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

: "${IMAGE:?set IMAGE}"
tmp="$(mktemp)"
trap 'rm -f "${tmp}"' EXIT
IMAGE="${IMAGE}" OUT="${tmp}" ./hack/render-acm-policy.sh

cat "${tmp}"
echo "---"
cat deploy/acm/placement-hub-fleet-validator.yaml
echo "---"
cat deploy/acm/policy-spoke-probes.yaml

#!/usr/bin/env bash
# One-time: register the fleet-validator Argo CD Application on a cluster whose OpenShift GitOps
# already has repo access to lmcdasm/dasmlab-live-cicd. After this, every push to main rolls out
# through CI -> live-cicd -> Argo CD.
#   OC_CONTEXT=<ctx> ./scripts/bootstrap-argocd.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OC="oc${OC_CONTEXT:+ --context=${OC_CONTEXT}}"

${OC} whoami >/dev/null
${OC} get namespace openshift-gitops >/dev/null || { echo "OpenShift GitOps not installed"; exit 1; }

${OC} apply -f "${ROOT}/k8s_envelope/argocd-application.yaml"

echo "Watch it:"
echo "  ${OC} -n openshift-gitops get application fleet-validator"
echo "  ${OC} -n fleet-validator get pods,route"

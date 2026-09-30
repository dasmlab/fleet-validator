#!/usr/bin/env bash
# One-time: register the fleet-validator Argo CD Application on a hub that already has
# OpenShift GitOps with repo access to lmcdasm/dasmlab-live-cicd (see that repo's bootstrap
# scripts). After this, every push to main here rolls out through CI -> live-cicd -> Argo -> ACM.
#   OC_CONTEXT=<ctx> ./scripts/bootstrap-argocd.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OC="oc${OC_CONTEXT:+ --context=${OC_CONTEXT}}"

${OC} whoami >/dev/null
${OC} get namespace openshift-gitops >/dev/null || { echo "OpenShift GitOps not installed"; exit 1; }
${OC} get namespace open-cluster-management-global-set >/dev/null || { echo "ACM global set namespace missing"; exit 1; }

${OC} apply -f "${ROOT}/k8s_envelope/argocd-rbac.yaml"
${OC} apply -f "${ROOT}/k8s_envelope/argocd-application.yaml"

echo "Watch it:"
echo "  ${OC} -n openshift-gitops get application fleet-validator"
echo "  ${OC} -n open-cluster-management-global-set get policy"
echo "  ${OC} -n fleet-validator get pods,route"

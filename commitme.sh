#!/usr/bin/env bash
# Commit + SemVer tag (point|minor|major). Same pattern as s3-mon / rf2vc.
# Re-renders the dashboards and the hub policy so the policy pins the tag being cut.
set -euo pipefail
trap 'echo "Script failed at line $LINENO: $BASH_COMMAND"' ERR

BUMP_TYPE="${1:-}"
shift || true
MSG="${*:-}"

if [[ -z "${BUMP_TYPE}" || -z "${MSG}" ]]; then
  echo "Usage: ./commitme.sh [point|minor|major] \"commit message\""
  exit 1
fi

echo "Pulling latest commits and tags..."
git pull origin "$(git rev-parse --abbrev-ref HEAD)" --tags 2>/dev/null || true

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
LATEST_TAG="$(git tag -l 'v*.*.*' '*.*.*' | grep -E '^v?[0-9]+\.[0-9]+\.[0-9]+$' | sed 's/^v//' | sort -V | tail -n 1 || true)"
if [[ -z "${LATEST_TAG}" && -f .localbuild ]]; then
  LATEST_TAG="$(tr -d '[:space:]' < .localbuild)"
  LATEST_TAG="${LATEST_TAG#v}"
fi

if [[ -z "${LATEST_TAG}" ]]; then
  MAJOR=0; MINOR=0; PATCH=0
else
  IFS='.' read -r MAJOR MINOR PATCH <<<"${LATEST_TAG}"
  MAJOR=$((10#${MAJOR:-0}))
  MINOR=$((10#${MINOR:-0}))
  PATCH=$((10#${PATCH:-0}))
fi

echo "Current version: ${MAJOR}.${MINOR}.${PATCH}"

case "${BUMP_TYPE}" in
  major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  point|patch) PATCH=$((PATCH + 1)) ;;
  *)
    echo "Invalid bump type: ${BUMP_TYPE} (want point|minor|major)"
    exit 1
    ;;
esac

NEW_TAG="v${MAJOR}.${MINOR}.${PATCH}"
echo "New version: ${NEW_TAG}"
echo "${MAJOR}.${MINOR}.${PATCH}" > .localbuild

python3 hack/gen-dashboards.py
./hack/render-grafana-configmap.sh
VERSION="${NEW_TAG}" ./hack/render-acm-policy.sh

git add -A .
if git diff --cached --quiet; then
  echo "No staged changes. Aborting."
  exit 1
fi

git commit -m "${MSG}"
git tag -a "${NEW_TAG}" -m "${MSG}"
# Atomic: CI must see the tag on HEAD, or it bumps and claims a version of its own.
git push --atomic origin "${BRANCH}" "${NEW_TAG}"
echo "Done: ${NEW_TAG} on ${BRANCH}"

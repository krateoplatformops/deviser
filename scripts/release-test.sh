#!/usr/bin/env bash

set -euo pipefail

TAG="${1:-1.2.3}"
CLUSTER_NAME="kind"
REPO="ghcr.io/$(git config --get remote.origin.url | sed -E 's#.*[:/](.*)/(.*)\.git#\1/\2#')"
IMAGE="${REPO}:${TAG}"

echo "Testing release image locally"
echo " Image: ${IMAGE}"
echo " Kind cluster: ${CLUSTER_NAME}"
echo

# Build release image via act (no push)
echo "Running act build..."
act push \
  -W .github/workflows/release-image.yml \
  --env GITHUB_REF=refs/tags/${TAG} \
  --env DOCKER_PUSH=false \
  --env LOCAL_KIND=true

# Load images into kind
echo
echo "Loading images into kind..."
kind load docker-image "${IMAGE}" "${REPO}:latest" --name "${CLUSTER_NAME}"

# Verify nodes
echo
echo "Verifying image in cluster..."
kubectl get nodes -o wide > /dev/null

# Deploy manifests con Helm template
echo
echo "Deploying manifests..."
helm template deviser ./chart \
  -n demo-system \
  --set image.repository=${REPO} \
  --set image.tag=${TAG} \
  --set image.pullPolicy=Never \
  -f "./chart/values.dev.yaml" | kubectl apply -f -

echo
echo "All tests passed!"
echo "👉 You can now run: git tag ${TAG} && git push --tags"

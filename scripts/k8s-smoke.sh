#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-kind}"
NAMESPACE="${K8S_SMOKE_NAMESPACE:-gpu-telemetry-smoke}"
RELEASE_NAME="${K8S_SMOKE_RELEASE:-gpu-telemetry-smoke}"
IMAGE_REPOSITORY="${K8S_SMOKE_IMAGE_REPOSITORY:-gpu-telemetry-pipeline}"
CHART_DIR="$ROOT_DIR/deployments/helm/gpu-telemetry"
PORT_FORWARD_PID=""

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Required command not found: $1" >&2
    exit 1
  }
}

for command in docker kind kubectl helm curl jq; do
  require_command "$command"
done

CURRENT_CONTEXT="$(kubectl config current-context)"
EXPECTED_CONTEXT="kind-$KIND_CLUSTER_NAME"
if [[ "$CURRENT_CONTEXT" != "$EXPECTED_CONTEXT" && "${K8S_SMOKE_ALLOW_NON_KIND:-false}" != "true" ]]; then
  echo "Refusing to deploy into context '$CURRENT_CONTEXT'." >&2
  echo "This smoke test is intended for '$EXPECTED_CONTEXT'. Set K8S_SMOKE_ALLOW_NON_KIND=true only for a disposable test cluster." >&2
  exit 1
fi

cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n "$PORT_FORWARD_PID" ]]; then
    kill "$PORT_FORWARD_PID" >/dev/null 2>&1 || true
  fi
  if [[ "$status" -ne 0 ]]; then
    echo "Kubernetes smoke test failed; collecting workload diagnostics." >&2
    kubectl -n "$NAMESPACE" get pods,deployments,services,hpa 2>/dev/null || true
    kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp 2>/dev/null | tail -40 || true
  fi
  if [[ "${K8S_SMOKE_KEEP_RESOURCES:-false}" != "true" ]]; then
    helm uninstall "$RELEASE_NAME" --namespace "$NAMESPACE" >/dev/null 2>&1 || true
    kubectl delete namespace "$NAMESPACE" --wait=false >/dev/null 2>&1 || true
  else
    echo "Keeping Kubernetes smoke resources in namespace '$NAMESPACE'."
  fi
  exit "$status"
}
trap cleanup EXIT

cd "$ROOT_DIR"

echo "Building application images for Kind..."
docker build -f deployments/docker/Dockerfile --build-arg APP=api -t "$IMAGE_REPOSITORY:api" .
docker build -f deployments/docker/Dockerfile --build-arg APP=streamer -t "$IMAGE_REPOSITORY:streamer" .
docker build -f deployments/docker/Dockerfile --build-arg APP=collector -t "$IMAGE_REPOSITORY:collector" .

echo "Loading application images into Kind cluster '$KIND_CLUSTER_NAME'..."
kind load docker-image --name "$KIND_CLUSTER_NAME" "$IMAGE_REPOSITORY:api"
kind load docker-image --name "$KIND_CLUSTER_NAME" "$IMAGE_REPOSITORY:streamer"
kind load docker-image --name "$KIND_CLUSTER_NAME" "$IMAGE_REPOSITORY:collector"

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply --namespace "$NAMESPACE" -f tests/k8s/postgres.yaml
kubectl rollout status deployment/postgres --namespace "$NAMESPACE" --timeout=180s

kubectl create secret generic gpu-telemetry-db \
  --namespace "$NAMESPACE" \
  --from-literal=DATABASE_URL='postgres://gpu_telemetry:gpu_telemetry_smoke@postgres:5432/gpu_telemetry?sslmode=disable' \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Installing Helm chart into the Kind cluster..."
helm upgrade --install "$RELEASE_NAME" "$CHART_DIR" \
  --namespace "$NAMESPACE" \
  --set image.repository="$IMAGE_REPOSITORY" \
  --set image.pullPolicy=Never \
  --set image.tags.api=api \
  --set image.tags.streamer=streamer \
  --set image.tags.collector=collector \
  --set streamer.interval=50ms \
  --wait --timeout 240s

for component in api streamer collector; do
  deployment="$RELEASE_NAME-gpu-telemetry-$component"
  kubectl rollout status "deployment/$deployment" --namespace "$NAMESPACE" --timeout=180s
  hpa="$deployment"
  max_replicas="$(kubectl get hpa "$hpa" --namespace "$NAMESPACE" -o jsonpath='{.spec.maxReplicas}')"
  if [[ "$max_replicas" != "10" ]]; then
    echo "Expected HPA '$hpa' maxReplicas=10, got '$max_replicas'." >&2
    exit 1
  fi
done

API_SERVICE="$RELEASE_NAME-gpu-telemetry-api"
kubectl port-forward --namespace "$NAMESPACE" "service/$API_SERVICE" 18080:80 >/tmp/gpu-telemetry-k8s-port-forward.log 2>&1 &
PORT_FORWARD_PID=$!

ready=false
for _ in $(seq 1 60); do
  if curl --silent --show-error --fail http://127.0.0.1:18080/readyz >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 2
done
if [[ "$ready" != "true" ]]; then
  echo "API did not become ready through the Kubernetes Service." >&2
  cat /tmp/gpu-telemetry-k8s-port-forward.log >&2 || true
  exit 1
fi

has_gpu=false
for _ in $(seq 1 60); do
  response="$(curl --silent --show-error --fail http://127.0.0.1:18080/api/v1/gpus 2>/dev/null || true)"
  if [[ -n "$response" ]] && printf '%s' "$response" | jq -e '.count > 0' >/dev/null 2>&1; then
    has_gpu=true
    break
  fi
  sleep 2
done
if [[ "$has_gpu" != "true" ]]; then
  echo "No GPU telemetry reached the API before the timeout." >&2
  exit 1
fi

echo "Kind deployment smoke test passed: API, Streamer, Collector, PostgreSQL, telemetry flow and all three HPA caps were verified."

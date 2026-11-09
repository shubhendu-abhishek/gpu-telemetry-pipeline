#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-kind}"
NAMESPACE="${K8S_SMOKE_NAMESPACE:-gpu-telemetry-smoke}"
RELEASE_NAME="${K8S_SMOKE_RELEASE:-gpu-telemetry-smoke}"
IMAGE_REPOSITORY="${K8S_SMOKE_IMAGE_REPOSITORY:-gpu-telemetry-pipeline}"
METRICS_SERVER_VERSION="${METRICS_SERVER_VERSION:-v0.8.1}"
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
make docker-build APP_NAME="$IMAGE_REPOSITORY"

echo "Loading application images into Kind cluster '$KIND_CLUSTER_NAME'..."
kind load docker-image --name "$KIND_CLUSTER_NAME" "$IMAGE_REPOSITORY:api"
kind load docker-image --name "$KIND_CLUSTER_NAME" "$IMAGE_REPOSITORY:streamer"
kind load docker-image --name "$KIND_CLUSTER_NAME" "$IMAGE_REPOSITORY:collector"

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply --namespace "$NAMESPACE" -f tests/k8s/postgres.yaml
kubectl rollout status deployment/postgres --namespace "$NAMESPACE" --timeout=180s

echo "Installing Metrics Server ${METRICS_SERVER_VERSION} for runtime HPA checks..."
kubectl apply -f "https://github.com/kubernetes-sigs/metrics-server/releases/download/${METRICS_SERVER_VERSION}/components.yaml"
# Kind's kubelet certificates are self-signed. This bypass is confined to the disposable Kind test cluster.
kubectl patch deployment metrics-server --namespace kube-system --type=json \
  --patch='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}'
kubectl rollout status deployment/metrics-server --namespace kube-system --timeout=180s

metrics_ready=false
for _ in $(seq 1 30); do
  if kubectl top nodes >/dev/null 2>&1; then
    metrics_ready=true
    break
  fi
  sleep 10
done
if [[ "$metrics_ready" != "true" ]]; then
  echo "Metrics Server did not publish resource metrics." >&2
  kubectl describe deployment metrics-server --namespace kube-system >&2 || true
  exit 1
fi

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
  --set api.autoscaling.scaleDownStabilizationWindowSeconds=30 \
  --set streamer.autoscaling.scaleDownStabilizationWindowSeconds=30 \
  --set collector.autoscaling.scaleDownStabilizationWindowSeconds=30 \
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

test_hpa_runtime() {
  local component="$1"
  local deployment="$RELEASE_NAME-gpu-telemetry-$component"
  local hpa="$deployment"
  local min_replicas
  local baseline
  local desired
  local available
  local scaled_up=false
  local scaled_down=false
  local sidecar_index

  min_replicas="$(kubectl get hpa "$hpa" --namespace "$NAMESPACE" -o jsonpath='{.spec.minReplicas}')"
  baseline="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o jsonpath='{.spec.replicas}')"
  baseline="${baseline:-0}"
  echo "Testing runtime HPA for $component (baseline=$baseline, minimum=$min_replicas, maximum=10)..."

  # Temporary CPU sidecar contributes to the Pod's Resource metric. It is removed
  # once scale-up is observed, after which the HPA should return to minReplicas.
  kubectl patch deployment "$deployment" --namespace "$NAMESPACE" --type=strategic --patch='{
    "spec":{"template":{"spec":{"containers":[{
      "name":"autoscaling-load",
      "image":"busybox:1.36.1",
      "imagePullPolicy":"IfNotPresent",
      "command":["/bin/sh","-c","while true; do :; done"],
      "resources":{"requests":{"cpu":"10m","memory":"8Mi"},"limits":{"cpu":"200m","memory":"32Mi"}}
    }]}}}
  }'
  kubectl rollout status "deployment/$deployment" --namespace "$NAMESPACE" --timeout=180s

  for _ in $(seq 1 30); do
    desired="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o jsonpath='{.spec.replicas}')"
    available="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o jsonpath='{.status.availableReplicas}')"
    desired="${desired:-0}"
    available="${available:-0}"
    if [[ "$desired" =~ ^[0-9]+$ && "$available" =~ ^[0-9]+$ ]] &&
      (( desired > baseline && available >= desired )); then
      scaled_up=true
      break
    fi
    sleep 10
  done

  if [[ "$scaled_up" != "true" ]]; then
    echo "HPA for $component did not scale above baseline=$baseline under CPU load." >&2
    kubectl describe hpa "$hpa" --namespace "$NAMESPACE" >&2 || true
    kubectl top pods --namespace "$NAMESPACE" --containers >&2 || true
    exit 1
  fi
  echo "HPA scale-up observed for $component: $baseline -> $desired replicas."

  sidecar_index="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o json |
    jq -r '.spec.template.spec.containers | map(.name) | index("autoscaling-load")')"
  if [[ "$sidecar_index" == "null" || -z "$sidecar_index" ]]; then
    echo "Could not find temporary HPA load sidecar for $component." >&2
    exit 1
  fi
  kubectl patch deployment "$deployment" --namespace "$NAMESPACE" --type=json \
    --patch="[\"op\":\"remove\",\"path\":\"/spec/template/spec/containers/$sidecar_index\"]"
  kubectl rollout status "deployment/$deployment" --namespace "$NAMESPACE" --timeout=180s

  # CI overrides production's 300-second scale-down stabilization to 30 seconds.
  for _ in $(seq 1 42); do
    desired="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o jsonpath='{.spec.replicas}')"
    available="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o jsonpath='{.status.availableReplicas}')"
    desired="${desired:-0}"
    available="${available:-0}"
    if [[ "$desired" == "$min_replicas" && "$available" == "$min_replicas" ]]; then
      scaled_down=true
      break
    fi
    sleep 10
  done

  if [[ "$scaled_down" != "true" ]]; then
    echo "HPA for $component did not return to minReplicas=$min_replicas after removing CPU load." >&2
    kubectl describe hpa "$hpa" --namespace "$NAMESPACE" >&2 || true
    kubectl get deployment "$deployment" --namespace "$NAMESPACE" >&2 || true
    exit 1
  fi
  echo "HPA scale-down observed for $component: returned to $min_replicas replicas."
}

for component in api streamer collector; do
  test_hpa_runtime "$component"
done

echo "Kind deployment smoke test passed: telemetry flow and live HPA scale-up/scale-down verified for API, Streamer and Collector; all HPA maxima are 10."

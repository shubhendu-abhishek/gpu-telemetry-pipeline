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
METRICS_SERVER_INSTALLED_BY_TEST=false

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
    for component in api streamer collector; do
      while IFS= read -r pod; do
        [[ -z "$pod" ]] && continue
        echo "--- Previous $component logs: $pod ---" >&2
        kubectl -n "$NAMESPACE" logs "$pod" -c "$component" --previous --tail=60 2>&1 || true
        echo "--- Current $component logs: $pod ---" >&2
        kubectl -n "$NAMESPACE" logs "$pod" -c "$component" --tail=40 2>&1 || true
      done < <(kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/component=$component" -o json 2>/dev/null |
        jq -r '.items[].metadata.name' 2>/dev/null || true)
    done
    kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp 2>/dev/null | tail -40 || true
  fi
  if [[ "${K8S_SMOKE_KEEP_RESOURCES:-false}" != "true" ]]; then
    helm uninstall "$RELEASE_NAME" --namespace "$NAMESPACE" >/dev/null 2>&1 || true
    kubectl delete namespace "$NAMESPACE" --wait=false >/dev/null 2>&1 || true
    if [[ "$METRICS_SERVER_INSTALLED_BY_TEST" == "true" ]]; then
      kubectl delete -f "https://github.com/kubernetes-sigs/metrics-server/releases/download/${METRICS_SERVER_VERSION}/components.yaml" --ignore-not-found=true >/dev/null 2>&1 || true
    fi
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

if ! kubectl get deployment metrics-server --namespace kube-system >/dev/null 2>&1; then
  echo "Installing Metrics Server ${METRICS_SERVER_VERSION} for runtime HPA checks..."
  METRICS_SERVER_INSTALLED_BY_TEST=true
  kubectl apply -f "https://github.com/kubernetes-sigs/metrics-server/releases/download/${METRICS_SERVER_VERSION}/components.yaml"
  # Kind's kubelet certificates are self-signed. This bypass is confined to the disposable Kind test cluster.
  kubectl patch deployment metrics-server --namespace kube-system --type=json \
    --patch='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
fi
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
  local baseline=0
  local desired=0
  local available=0
  local selector
  local scaled_up=false
  local scaled_down=false
  local pod
  local pid
  local -A load_pids=()

  min_replicas="$(kubectl get hpa "$hpa" --namespace "$NAMESPACE" -o jsonpath='{.spec.minReplicas}')"

  # HPA reconciliation starts after Helm creates the Deployments. Wait until the
  # configured minimum is reached so the initial 1 -> min correction is never
  # misreported as load-driven scale-up.
  for _ in $(seq 1 30); do
    desired="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o jsonpath='{.spec.replicas}')"
    available="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o jsonpath='{.status.availableReplicas}')"
    desired="${desired:-0}"
    available="${available:-0}"
    if [[ "$desired" == "$min_replicas" && "$available" == "$min_replicas" ]]; then
      baseline="$desired"
      break
    fi
    sleep 5
  done

  if [[ "$baseline" != "$min_replicas" ]]; then
    echo "HPA for $component did not stabilize at minReplicas=$min_replicas before load testing." >&2
    kubectl describe hpa "$hpa" --namespace "$NAMESPACE" >&2 || true
    kubectl get deployment "$deployment" --namespace "$NAMESPACE" >&2 || true
    exit 1
  fi

  selector="$(kubectl get deployment "$deployment" --namespace "$NAMESPACE" -o json |
    jq -r '.spec.selector.matchLabels | to_entries | map("\(.key)=\(.value)") | join(",")')"
  echo "Testing runtime HPA for $component (stable baseline=$baseline, minimum=$min_replicas, maximum=10)..."
  first_pod="$(kubectl get pods --namespace "$NAMESPACE" -l "$selector" -o jsonpath='{.items[0].metadata.name}')"
  if [[ -z "$first_pod" ]] || ! kubectl exec --namespace "$NAMESPACE" "$first_pod" -c "$component" -- \
    sh -c 'command -v nohup >/dev/null && command -v timeout >/dev/null && command -v nice >/dev/null && command -v yes >/dev/null'; then
    echo "The $component image must provide nohup, timeout, nice and yes for the bounded HPA load test." >&2
    exit 1
  fi

  start_cpu_load() {
    local target_pod="$1"
    [[ -n "${load_pids[$target_pod]:-}" ]] && return 0

    # Run a detached CPU loop inside the existing application container. This
    # avoids changing the Pod template or restarting the Go process being tested.
    pid="$(kubectl exec --namespace "$NAMESPACE" "$target_pod" -c "$component" -- \
      sh -c 'nohup sh -c "while :; do timeout 1 nice -n 19 yes >/dev/null 2>&1 || true; sleep 4; done" >/dev/null 2>&1 </dev/null & echo $!' 2>/dev/null || true)"
    if [[ "$pid" =~ ^[0-9]+$ ]]; then
      load_pids["$target_pod"]="$pid"
      if ! kubectl exec --namespace "$NAMESPACE" "$target_pod" -c "$component" -- sh -c "kill -0 $pid" >/dev/null 2>&1; then
        echo "CPU load worker exited immediately in pod $target_pod (pid=$pid)." >&2
        return 1
      fi
      echo "Started bounded CPU load in $component pod $target_pod (pid=$pid; 1s active / 4s idle; nice=19)."
    fi
  }

  stop_cpu_load() {
    local target_pod
    for target_pod in "${!load_pids[@]}"; do
      kubectl exec --namespace "$NAMESPACE" "$target_pod" -c "$component" -- \
        sh -c "kill ${load_pids[$target_pod]}" >/dev/null 2>&1 || true
    done
    load_pids=()
  }

  # Keep adding load to newly scheduled replicas during scale-up. The load
  # consumes CPU in the target container and is removed without a Deployment rollout.
  for _ in $(seq 1 36); do
    while IFS= read -r pod; do
      [[ -z "$pod" ]] && continue
      if kubectl wait --for=condition=Ready "pod/$pod" --namespace "$NAMESPACE" --timeout=1s >/dev/null 2>&1; then
        start_cpu_load "$pod"
      fi
    done < <(kubectl get pods --namespace "$NAMESPACE" -l "$selector" -o json |
      jq -r '.items[] | select(.status.phase == "Running") | .metadata.name')

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
    stop_cpu_load
    echo "HPA for $component did not scale above stable baseline=$baseline under CPU load." >&2
    kubectl describe hpa "$hpa" --namespace "$NAMESPACE" >&2 || true
    kubectl top pods --namespace "$NAMESPACE" --containers >&2 || true
    exit 1
  fi
  echo "HPA scale-up observed for $component: $baseline -> $desired replicas."
  stop_cpu_load

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

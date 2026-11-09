# Production cloud deployment runbook

This runbook describes how to deploy the application workloads to an existing EKS, GKE, AKS or other conformant Kubernetes cluster. The repository does **not** create the cloud account, Kubernetes cluster, managed PostgreSQL instance, DNS, ingress controller, certificates or cloud network. Those resources must be provisioned and approved by the platform owner.

## 1. Production topology and security boundary

- Kubernetes runs three application Deployments: API, Streamer and Collector.
- PostgreSQL is an external managed service. The queue and telemetry tables share that database.
- The chart creates ClusterIP Services, HPAs and a PodDisruptionBudget. No public LoadBalancer or Ingress is created by default.
- CPU-based HPAs require a functioning Metrics Server / Resource Metrics API.
- The API currently has no authentication middleware. Keep it on a private network and do not expose it directly to the public internet. Add authentication/authorization and a reviewed TLS ingress or API gateway before any public exposure.

## 2. Cloud resources required before deployment

Provision and verify the following in the chosen cloud account:

1. **Kubernetes:** a supported managed Kubernetes cluster, worker node capacity for the configured minimum replicas, namespace-scoped deployment permissions and a tested kubectl context.
2. **PostgreSQL:** PostgreSQL 15 or newer with backups/PITR, TLS, private networking, a dedicated least-privilege database/user and connection capacity for up to 10 replicas of each application component.
3. **Container registry:** the API, Streamer and Collector images published to GHCR. The CI workflow publishes immutable tags with the full Git commit SHA. For private GHCR packages, configure the optional pull credentials described below.
4. **Metrics:** Metrics Server installed and healthy. kubectl top nodes and kubectl top pods -n gpu-telemetry-prod must return current metrics. Configure the cloud cluster autoscaler separately; an HPA creates Pods but does not provision nodes.
5. **Network and identity:** private route from the application Pods to PostgreSQL; firewall/security-group rules; DNS and TLS only for a reviewed private gateway; least-privilege Kubernetes identity; audit logging and network policies.
6. **Operations:** alerts for API readiness/latency, DB availability and connections, queue depth/age, retries, dead-letter count, CPU/memory and HPA desired/current replicas.

Do not use the Compose database password in a cloud environment.

## 3. Configure GitHub production environment

In GitHub repository Settings → Environments, create an environment named production. Configure required reviewers and restrict deployment branches to main. Store these environment secrets:

| Secret | Required | Purpose |
|---|---:|---|
| KUBE_CONFIG | Yes | Kubeconfig for the target cluster, using a deployment identity scoped to the namespace |
| DATABASE_URL | Yes | TLS-enabled managed PostgreSQL connection string |
| GHCR_USERNAME | Only for private images | GHCR username for image pull |
| GHCR_TOKEN | Only for private images | Read-only GHCR package token |

Prefer short-lived credentials issued by your cloud identity provider and rotate any credential stored as a GitHub secret. The current provider-neutral workflow accepts a kubeconfig secret; replace that step with the cloud's OIDC/workload-identity login action when standardizing on a specific provider. Never commit kubeconfig or database credentials.

## 4. Verify a release image exists

The container-images job publishes these tags after all required CI jobs pass on a push to main:

- ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:sha-<full-commit-sha>-api
- ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:sha-<full-commit-sha>-streamer
- ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:sha-<full-commit-sha>-collector

Use the exact 40-character commit SHA that has successful CI and published images. Do not deploy the mutable latest tags to production.

## 5. Run the production workflow

After the cloud resources and GitHub environment are configured:

1. Open Actions → **Production deployment**.
2. Select the main branch and choose **Run workflow**.
3. Supply the full 40-character image commit SHA. The default is the workflow commit SHA.
4. Supply the target Kubernetes namespace (default: gpu-telemetry-prod).
5. Approve the protected production environment deployment when required.

The workflow:
- Verifies the selected branch and commit SHA.
- Configures Kubernetes access from the protected environment secret.
- Requires live Metrics Server data before deploying.
- Creates/updates the namespace and database Secret without printing the DSN.
- Adds a read-only GHCR pull Secret only when private-package credentials are configured.
- Installs/upgrades Helm with immutable image tags and production values using atomic wait.
- Waits for all three Deployments, checks every HPA maximum is 10 and checks API /readyz through a loopback-only port-forward.
- Leaves the API ClusterIP-only and does not configure public ingress.

A failed Helm rollout uses Helm's atomic rollback behavior. For a later rollback, rerun the workflow with the previous known-good 40-character image commit SHA.

## 6. Production scaling policy

The production values file configures two minimum replicas and a maximum of ten for API, Streamer and Collector. The CPU utilization target is 70%, with a 300-second HPA scale-down stabilization window to reduce oscillation. Validate actual capacity and database connection limits before raising the replica cap or changing CPU requests/limits.

HPA scales Pods based on CPU metrics; it does not prove queue throughput is sufficient by itself. Monitor queue age/depth and PostgreSQL saturation. If backlog grows while CPU remains low, consider a custom queue-depth metric (for example, via Prometheus Adapter) rather than relying only on CPU.

## 7. Post-deployment acceptance checks

Run these from an authorized workstation using the same context and namespace:

~~~bash
kubectl -n gpu-telemetry-prod get deployments,pods,services,hpa,pdb
kubectl -n gpu-telemetry-prod get hpa
kubectl -n gpu-telemetry-prod top pods --containers
kubectl -n gpu-telemetry-prod get events --sort-by=.lastTimestamp
~~~

Forward the private API only from a trusted workstation:

~~~bash
kubectl -n gpu-telemetry-prod port-forward service/gpu-telemetry-gpu-telemetry-api 8080:80
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/api/v1/gpus
~~~

Confirm the expected GPU inventory and telemetry, database TLS, backups, alerts, HPA metrics, node capacity and network isolation. Record the cloud provider, cluster version, deployment commit, image digests, rollout time and observed scaling data in the operations record.

## 8. What is not automated by this repository

- Cloud account/project/subscription creation and infrastructure provisioning (VPC/VNet, subnets, cluster, node groups, NAT, firewall, managed PostgreSQL, backups, DNS, certificates and monitoring).
- Provider-specific OIDC/workload identity setup; the current generic workflow uses a protected KUBE_CONFIG secret.
- Public API exposure. This API has no authentication, so public ingress is intentionally not created.
- A production load test or a production HPA scale-up/down measurement. The Kind CI smoke test performs a disposable CPU-load test; production capacity still needs a controlled, approved load test.
- A formal security review, disaster-recovery exercise or production SLO sign-off.

Treat a successful workflow as a deployment to the configured target cluster, not as proof that the cloud infrastructure itself is resilient or production-ready.

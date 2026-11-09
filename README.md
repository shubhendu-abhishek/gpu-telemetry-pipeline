# Elastic GPU Telemetry Pipeline with Custom Message Queue

A Golang pipeline that reads independent DCGM telemetry datapoints from CSV, publishes them to a custom durable queue, persists them in PostgreSQL, and exposes a REST API. The queue is implemented in Go and PostgreSQL; **Kafka, RabbitMQ, ZeroMQ and other external brokers are not used**.

This README is the end-to-end operator guide for local VM, Docker Compose, Kubernetes and cloud deployment.

## Installation & Deployment Roadmap

Follow these steps in order. Each step links to the detailed instructions later in this README.

1. **Install prerequisites** — install the required development and deployment tools and confirm the machine resources. See [Prerequisites](#4-prerequisites).
2. **Clone the repository and download dependencies** — clone the GitHub repository, check out the source and run `make bootstrap`. See [Initial checkout and configuration](#5-initial-checkout-and-configuration).
3. **Configure PostgreSQL and environment variables** — create the application database/user for a VM deployment and set the DSN, CSV path, source ID and service settings. For Compose, the development database is configured in the Compose file. See [Environment variables](#53-environment-variables) and [PostgreSQL VM setup](#71-install-postgresql-and-create-a-database).
4. **Run locally using Docker Compose** — start PostgreSQL, the API Gateway, Streamer and Collectors with `make compose-up`. See [Run locally with Docker Compose](#6-run-locally-with-docker-compose-recommended-first-run).
5. **Verify APIs and telemetry processing** — check health/readiness, list GPUs and query telemetry for a GPU UUID. See [REST API usage](#10-rest-api-usage).
6. **Execute unit, integration and E2E tests** — run Go unit tests, enforce the 90% core-package coverage gate, execute PostgreSQL integration tests and run the live Compose E2E test. See [Testing and quality gates](#11-testing-and-quality-gates).
7. **Build Docker images** — build the API, Streamer and Collector images for local or registry deployment. See [Build and deploy with Docker](#8-build-and-deploy-with-docker).
8. **Deploy to Kubernetes using Helm** — validate the chart, configure the database Secret, set image values and install the release. See [Kubernetes and cloud deployment with Helm](#9-kubernetes-and-cloud-deployment-with-helm).
9. **Configure cloud infrastructure and monitoring** — provision the cluster, managed PostgreSQL, registry/network access, TLS, secrets, Metrics Server, backups and operational monitoring. See [Cloud prerequisites](#43-gpu-and-cloud-prerequisites), [Prepare the cluster and external database](#91-prepare-the-cluster-and-external-database) and [Security and production checklist](#14-security-and-production-checklist).

### Quick local start

For the fastest development run, Docker Compose starts the complete local stack. The host needs Git, Docker Engine with the Compose v2 plugin, and GNU Make. Go and PostgreSQL are supplied inside the containers for this path; a physical GPU, CUDA toolkit and NVIDIA driver are not required because the application reads the supplied CSV.

```bash
git clone https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline.git
cd gpu-telemetry-pipeline

make doctor
make compose-up

# Wait for readiness, then verify the API
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8080/api/v1/gpus
```

- Swagger UI: [http://localhost:8080/swagger](http://localhost:8080/swagger)
- OpenAPI JSON: [http://localhost:8080/openapi.json](http://localhost:8080/openapi.json)

View logs and stop the stack:

```bash
docker compose -f deployments/docker-compose.yaml logs -f
make compose-down
```

`make compose-down` preserves the PostgreSQL volume. To delete the local database and start from an empty state, use `docker compose -f deployments/docker-compose.yaml down -v`.

## 1. Assignment coverage

| Requirement | Implementation |
|---|---|
| CSV Telemetry Streamer | `cmd/streamer`, `internal/streamer`, `internal/telemetry` |
| Scalable Streamers (up to 10) | Shared transactional byte-offset cursor; Helm HPA |
| Custom message queue | PostgreSQL-backed Go queue: claim, lease, acknowledge, retry and dead-letter |
| Scalable Collectors (up to 10) | Concurrent workers and PostgreSQL `FOR UPDATE SKIP LOCKED`; Helm HPA |
| REST API Gateway | `cmd/api`, `internal/api` |
| List GPUs | `GET /api/v1/gpus` |
| Query GPU telemetry | `GET /api/v1/gpus/{id}/telemetry` |
| Inclusive time filters | `start_time` and `end_time` filter `processed_at` inclusively |
| OpenAPI | Generated using `make openapi`; served at `/openapi.json` |
| Deployment | Docker, Compose, Kubernetes and Helm |
| Tests | Go unit tests, PostgreSQL integration tests, Docker Compose E2E; coverage target enforced at 90% |

## 2. Architecture

~~~mermaid
flowchart LR
 CSV[(CSV)] --> S[Streamer replicas]
 S --> Q[(Custom queue in PostgreSQL)]
 Q --> C[Collector replicas]
 C --> DB[(Telemetry + GPU inventory)]
 DB --> API[REST API Gateway]
 API --> Client[API client / Swagger UI]
~~~

Key design decisions:

- **GPU identity:** the CSV `uuid` is globally unique. `gpu_id` is a host-local index and can repeat across hosts.
- **Timestamps:** retain the CSV timestamp as `source_timestamp`; the Streamer sets `streamed_at`; the Collector sets UTC `processed_at`. API ordering and inclusive time filters use `processed_at`, as required by the assignment.
- **Queue delivery:** delivery is at-least-once. Message leases recover work after a Collector crash, and unique `message_id` makes telemetry persistence idempotent.
- **Streamer scaling:** queue insertion and source-cursor advancement occur in one PostgreSQL transaction. Replicas share the cursor so they do not independently replay the same file.
- **Collector scaling:** replicas claim disjoint messages with `FOR UPDATE SKIP LOCKED`. Failed messages retry with exponential backoff and eventually move to the dead-letter state.

## 3. Repository layout

| Path | Purpose |
|---|---|
| `cmd/api` | REST API process |
| `cmd/streamer` | CSV telemetry publisher |
| `cmd/collector` | Queue consumer and telemetry persistence |
| `cmd/openapi-gen` | OpenAPI JSON generator |
| `internal/api` | HTTP routes, API models and OpenAPI contract |
| `internal/collector` | Concurrent message processing |
| `internal/config` | Environment configuration and validation |
| `internal/postgres` | PostgreSQL queue, migrations and repositories |
| `internal/queue` | Queue interfaces |
| `internal/streamer` | CSV publishing loop |
| `internal/telemetry` | CSV parsing and telemetry model |
| `data/` | Supplied DCGM CSV input |
| `db/migrations`, `internal/postgres/migrations` | Database schema |
| `api/openapi.json` | Generated API contract |
| `deployments/docker` | Multi-target Dockerfile |
| `deployments/docker-compose.yaml` | Local multi-container environment |
| `deployments/helm/gpu-telemetry` | Kubernetes Helm chart |
| `tests/e2e` | Black-box pipeline test |
| `.github/workflows/ci.yaml` | GitHub Actions pipeline |
| `docs/` | Architecture, operations, validation, Git and AI-use notes |

## 4. Prerequisites

Choose the prerequisites for the deployment mode you intend to use. You do not need Kubernetes or Helm to run the application locally with Docker Compose.

### 4.1 Tool requirements

| Tool | Recommended / project version | Needed for |
|---|---|---|
| Go | 1.27.3 (same version used by CI and Docker builder) | Build and Go tests |
| Git | 2.40 or newer | Clone and source control |
| GNU Make | 4.x | Project commands |
| Docker Engine | 25 or newer | Images, Compose and E2E |
| Docker Compose | v2 plugin | Local container deployment and E2E |
| PostgreSQL | 15 or newer; Compose uses PostgreSQL 16 | Manual VM setup and database integration tests |
| kubectl | Compatible with the target Kubernetes cluster | Kubernetes deployment and inspection |
| Helm | 4.3.1 (chart deployment tooling) | Helm lint, render and install |
| curl | Any supported version | API smoke tests |
| jq | Optional | Formatting JSON API responses |

The module declares Go compatibility in `go.mod`; CI and the Docker build stage use Go 1.27.3. Use that version to reproduce the project build environment.

### 4.2 Machine sizing

For a development VM running the complete Compose stack, use **4 vCPU, 8 GB RAM and at least 20 GB free disk** as a practical starting point. These are recommendations, not hard application limits. A smaller machine may work with fewer replicas and a lower test load.

### 4.3 GPU and cloud prerequisites

- No physical GPU, CUDA toolkit or NVIDIA driver is required for this assignment. The supplied CSV simulates GPU telemetry.
- A local VM needs a supported Linux distribution, PostgreSQL (unless using Compose), Go, Git and Make.
- A cloud deployment needs a Kubernetes cluster (EKS, GKE, AKS or another conformant cluster), a reachable PostgreSQL database, a container registry, network access from the cluster to the database, TLS configuration, Kubernetes Secrets or an external secrets manager, and Metrics Server for CPU-based HPA.
- **The Helm chart deploys the application workloads; it does not provision PostgreSQL or the Kubernetes cluster.** Provision the database and cluster separately.
- The supplied CSV is copied into each application image. For a custom or large CSV, mount a shared/read-only volume and configure the chart's existing PVC settings.

### 4.4 Verify prerequisites

From the repository root, run:

~~~bash
make doctor
go version
git --version
make help
~~~

The doctor target checks Go and Git and reports whether Docker and Helm are installed. Docker and Helm are optional for a Go-only local run.

## 5. Initial checkout and configuration

### 5.1 Clone the repository

~~~bash
git clone https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline.git
cd gpu-telemetry-pipeline

# Confirm the dataset is present
ls -lh data/dcgm_metrics_20250718_134233.csv
~~~

The Streamer expects the supplied CSV at that path by default.

### 5.2 Download Go dependencies

~~~bash
make bootstrap
~~~

This runs `go mod download` and creates a local `.env` from `.env.example` when one does not already exist.

**Configuration note:** the Go processes read environment variables; they do not load a `.env` file themselves. For a manual VM run, export the values in your shell (or source the file as shown below). Docker Compose reads its project `.env` for Compose variable substitution, but only variables explicitly referenced by the Compose YAML are passed into containers. The current Compose file uses development database credentials and exposes `STREAMER_INTERVAL` and `COLLECTOR_WORKERS` as Compose overrides.

### 5.3 Environment variables

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | Local development PostgreSQL DSN | PostgreSQL connection URL |
| `HTTP_PORT` | `8080` | API listening port |
| `TELEMETRY_CSV_PATH` | `./data/dcgm_metrics_20250718_134233.csv` | CSV path read by Streamer |
| `TELEMETRY_SOURCE_ID` | `dcgm-metrics-sample` | Stable key for the shared CSV cursor; use a new ID for a different input file |
| `STREAMER_INTERVAL` | `500ms` | Delay between streamed datapoints |
| `COLLECTOR_WORKERS` | `4` (Compose sets `2`) | Worker goroutines per Collector process; range 1–10 |
| `COLLECTOR_BATCH_SIZE` | `25` | Maximum messages claimed per batch |
| `COLLECTOR_POLL_INTERVAL` | `500ms` (Compose sets `250ms`) | Delay when the queue is empty or a claim fails |
| `QUEUE_LEASE_DURATION` | `60s` | Message visibility lease |
| `QUEUE_MAX_ATTEMPTS` | `10` | Attempts before moving a message to dead-letter |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

For a manual VM deployment, copy and edit the sample environment file, then export it:

~~~bash
cp .env.example .env
# Edit .env for your database and local file paths.
set -a
. ./.env
set +a
~~~

Do not commit credentials or production connection strings. Add production secrets through the cloud secret manager or Kubernetes Secret.

## 6. Run locally with Docker Compose (recommended first run)

Docker Compose starts PostgreSQL, the API, Streamer and Collector. It uses development-only database credentials defined in `deployments/docker-compose.yaml`.

### 6.1 Start the application

~~~bash
make compose-up
~~~

The first start builds the three Go application images and starts PostgreSQL. PostgreSQL readiness is checked before the application containers start. The processes apply the embedded database migrations on startup.

Check container status and API readiness:

~~~bash
docker compose -f deployments/docker-compose.yaml ps
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
~~~

Open the API documentation:

- Swagger UI: http://localhost:8080/swagger
- OpenAPI JSON: http://localhost:8080/openapi.json

The GPU inventory may initially be empty. The Streamer publishes CSV records and Collectors persist them; query the GPU endpoint after the pipeline has had time to process data.

### 6.2 View logs

~~~bash
docker compose -f deployments/docker-compose.yaml logs -f postgres
docker compose -f deployments/docker-compose.yaml logs -f streamer
docker compose -f deployments/docker-compose.yaml logs -f collector
docker compose -f deployments/docker-compose.yaml logs -f api
~~~

### 6.3 Scale Streamers and Collectors

~~~bash
docker compose -f deployments/docker-compose.yaml up -d --build --scale streamer=3 --scale collector=4
docker compose -f deployments/docker-compose.yaml ps
~~~

The database cursor coordinates Streamers; queue row locks coordinate Collectors. The Helm chart's autoscalers are capped at 10 replicas per component.

### 6.4 Stop the application

~~~bash
make compose-down
~~~

This stops containers but **preserves the PostgreSQL volume**. To delete the local database and start from a clean state, run the destructive command below:

~~~bash
docker compose -f deployments/docker-compose.yaml down -v
~~~

## 7. Run on a Linux VM without Docker

Use this mode when you want to run the Go processes directly on a VM and connect to a locally installed or remote PostgreSQL database.

### 7.1 Install PostgreSQL and create a database

Install PostgreSQL 15 or newer using your Linux distribution's package manager, start the PostgreSQL service, and create an application user and database.

Example SQL (replace the example password):

~~~sql
CREATE USER gpu_telemetry WITH PASSWORD 'replace-with-a-strong-password';
CREATE DATABASE gpu_telemetry OWNER gpu_telemetry;
~~~

For a local-only development database, the DSN can use `sslmode=disable`. For a remote or cloud database, use TLS (for example `sslmode=require` or the provider's stricter recommended mode) and private networking.

### 7.2 Configure the application

~~~bash
git clone https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline.git
cd gpu-telemetry-pipeline
make bootstrap

export DATABASE_URL='postgres://gpu_telemetry:replace-with-a-strong-password@127.0.0.1:5432/gpu_telemetry?sslmode=disable'
export HTTP_PORT=8080
export TELEMETRY_CSV_PATH="$PWD/data/dcgm_metrics_20250718_134233.csv"
export TELEMETRY_SOURCE_ID=dcgm-metrics-sample
export STREAMER_INTERVAL=500ms
export COLLECTOR_WORKERS=4
export COLLECTOR_BATCH_SIZE=25
export COLLECTOR_POLL_INTERVAL=500ms
export QUEUE_LEASE_DURATION=60s
export QUEUE_MAX_ATTEMPTS=10
export LOG_LEVEL=info
~~~

### 7.3 Build and test before starting

~~~bash
make fmt
make vet
make lint
make test-unit
make coverage
make build
make openapi
~~~

### 7.4 Start the processes

Start each process in a separate terminal (or create separate systemd services). Ensure all terminals have the same database and CSV environment variables.

Terminal 1 – API:

~~~bash
go run ./cmd/api
~~~

Terminal 2 – Collector:

~~~bash
go run ./cmd/collector
~~~

Terminal 3 – Streamer:

~~~bash
go run ./cmd/streamer
~~~

The API, Streamer and Collector apply idempotent schema migrations when they start. Start PostgreSQL first. Starting Collectors before the Streamer is useful because messages can be consumed as soon as they are published.

## 8. Build and deploy with Docker

Build each application image from the repository root:

~~~bash
make docker-build
~~~

This creates these local images:

- `gpu-telemetry-pipeline:api`
- `gpu-telemetry-pipeline:streamer`
- `gpu-telemetry-pipeline:collector`

Alternatively, build one image explicitly:

~~~bash
docker build -f deployments/docker/Dockerfile --build-arg APP=api -t gpu-telemetry-pipeline:api .
docker build -f deployments/docker/Dockerfile --build-arg APP=streamer -t gpu-telemetry-pipeline:streamer .
docker build -f deployments/docker/Dockerfile --build-arg APP=collector -t gpu-telemetry-pipeline:collector .
~~~

The multi-stage Dockerfile uses Go 1.27.3 to build the binaries and runs them as a non-root user in the runtime image. The sample dataset is copied into the image at `/app/data`.

For the complete local environment, prefer `make compose-up` instead of running the containers individually because Compose also starts and configures PostgreSQL.

## 9. Kubernetes and cloud deployment with Helm

### 9.1 Prepare the cluster and external database

Before installing the chart:

1. Provision a Kubernetes cluster and configure the active context using kubectl.
2. Provision PostgreSQL 15+ (managed PostgreSQL is recommended for cloud).
3. Ensure the cluster can resolve and reach the PostgreSQL endpoint.
4. Configure database TLS and create an application database/user with least-privilege permissions.
5. Ensure Metrics Server is installed for CPU-based HPA.
6. Build and push the API, Streamer and Collector images to a registry accessible by the cluster.
7. Create a Kubernetes Secret for the database URL.

The chart does not deploy PostgreSQL. Do not use the local Compose database password in a cloud environment.

### 9.2 Build and publish application images

Build the images:

~~~bash
make docker-build
~~~

Example: tag and push the images to GitHub Container Registry (GHCR). Authenticate to GHCR first using an account/token with package write permission; do not put the token in the command history or a Git remote URL.

~~~bash
docker tag gpu-telemetry-pipeline:api ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:latest-api
docker tag gpu-telemetry-pipeline:streamer ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:latest-streamer
docker tag gpu-telemetry-pipeline:collector ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:latest-collector

docker push ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:latest-api
docker push ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:latest-streamer
docker push ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline:latest-collector
~~~

The GitHub Actions workflow is configured to publish these image tags to GHCR on a successful push to `main`. For private packages, configure image pull credentials in the cluster and set `image.pullSecrets` in Helm values.

### 9.3 Create namespace and database Secret

~~~bash
kubectl create namespace gpu-telemetry

kubectl -n gpu-telemetry create secret generic gpu-telemetry-db \
  --from-literal=DATABASE_URL='postgres://USER:PASSWORD@DB_HOST:5432/DB_NAME?sslmode=require'
~~~

Replace the placeholders with the managed PostgreSQL connection details. Avoid storing real passwords in committed Helm values files.

The chart expects an existing Secret named `gpu-telemetry-db` with the key `DATABASE_URL` by default. This is controlled by `database.existingSecret` and `database.secretKey` in `values.yaml`.

### 9.4 Validate and install the Helm chart

~~~bash
make helm-lint
make helm-template

helm upgrade --install gpu-telemetry deployments/helm/gpu-telemetry \
  --namespace gpu-telemetry --create-namespace \
  --set image.repository=ghcr.io/shubhendu-abhishek/gpu-telemetry-pipeline
~~~

Inspect workloads and autoscalers:

~~~bash
kubectl -n gpu-telemetry get deployments,pods,services,hpa,pdb
kubectl -n gpu-telemetry get events --sort-by=.lastTimestamp
~~~

Wait for the API deployment to become ready, then port-forward the API service. With the release and chart names used above, the service name is `gpu-telemetry-gpu-telemetry-api`:

~~~bash
kubectl -n gpu-telemetry port-forward service/gpu-telemetry-gpu-telemetry-api 8080:80
curl -fsS http://localhost:8080/readyz
~~~

### 9.5 Helm configuration and scaling

The default chart values configure 2 API, 2 Streamer and 2 Collector replicas. CPU-based HPAs can scale each component up to 10 replicas when Metrics Server is available. CPU requests and limits are configured in `values.yaml`.

Common settings:

| Helm value | Purpose |
|---|---|
| `image.repository` | Container registry/repository |
| `image.tags.api`, `image.tags.streamer`, `image.tags.collector` | Image tags for each process |
| `database.existingSecret` | Existing Kubernetes Secret containing the DSN |
| `api.replicaCount` | API replicas when autoscaling is disabled |
| `streamer.replicaCount` | Streamer replicas when autoscaling is disabled |
| `streamer.sourceID` | Shared CSV cursor key |
| `streamer.interval` | Streamer publishing interval |
| `streamer.existingClaim` | Optional existing PVC for a custom CSV |
| `collector.replicaCount` | Collector replicas when autoscaling is disabled |
| `collector.workers` | Worker goroutines per Collector pod |
| `*.autoscaling.minReplicas`, `*.autoscaling.maxReplicas` | HPA range (maximum is capped at 10) |

The default CSV is bundled in each image at `/app/data`; an additional volume is not required for the sample file. For a custom CSV, create a PVC containing the file, set `streamer.existingClaim`, `streamer.mountPath` and `streamer.csvFile` using a Helm values override. The chart references an existing PVC; it does not create one.

Uninstall the application:

~~~bash
helm uninstall gpu-telemetry --namespace gpu-telemetry
~~~

The database and its data are external to this Helm release and are not deleted by uninstalling the chart.

## 10. REST API usage

### 10.1 List GPUs

~~~bash
curl -sS http://localhost:8080/api/v1/gpus | jq
~~~

The response contains GPU UUID, index, device, model, hostname, first-seen and last-seen timestamps.

### 10.2 Query telemetry for a GPU

Use the globally unique GPU UUID returned by the inventory API:

~~~bash
GPU_UUID='GPU-...'
curl -sS "http://localhost:8080/api/v1/gpus/$GPU_UUID/telemetry" | jq
~~~

### 10.3 Inclusive time window and pagination

~~~bash
curl -sS 'http://localhost:8080/api/v1/gpus/GPU-.../telemetry?start_time=2026-11-09T08:00:00Z&end_time=2026-11-09T09:00:00Z&limit=100&offset=0' | jq
~~~

- Time values must use RFC3339 format.
- `start_time` and `end_time` are inclusive bounds on `processed_at`.
- `limit` defaults to 1000 and has a maximum of 5000.
- `offset` defaults to 0 and must be non-negative.
- Results are ordered by `processed_at` and then database row ID.

Operational endpoints:

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Process liveness |
| `GET /readyz` | PostgreSQL readiness |
| `GET /metrics` | Basic Prometheus text counter |
| `GET /openapi.json` | OpenAPI document |
| `GET /swagger` | Swagger UI |

## 11. Testing and quality gates

Run the checks from the repository root. Use the sequence below before opening a pull request.

### 11.1 Formatting, static analysis and unit tests

~~~bash
make fmt
make vet
make lint
make test-unit
~~~

- `make fmt` formats Go source and test files.
- `make vet` runs `go vet ./...`.
- `make lint` uses golangci-lint when installed; otherwise it falls back to `go vet`.
- `make test-unit` runs race-enabled tests for the core API, config, telemetry, Streamer and Collector packages.

### 11.2 Enforce 90% minimum core-package coverage

~~~bash
make coverage
make coverage-html
~~~

The coverage target creates `coverage.out`, prints per-function coverage and exits with a failure when aggregate statement coverage across the configured core packages is below 90%. The HTML report is written to `coverage.html`.

The 90% gate is not a claim that every package, including the PostgreSQL adapter, independently has 90% coverage. PostgreSQL behavior is exercised separately by integration tests against a live database.

### 11.3 PostgreSQL integration tests

Create a disposable test database and set `TEST_DATABASE_URL`:

~~~bash
export TEST_DATABASE_URL='postgres://gpu_telemetry:TEST_PASSWORD@127.0.0.1:5432/gpu_telemetry_test?sslmode=disable'
make test-postgres
~~~

These tests require a running PostgreSQL server and apply the repository migrations. Use a dedicated test database, not a production database.

### 11.4 End-to-end test

The E2E target requires Docker Engine and the Compose v2 plugin. It builds and starts PostgreSQL, API, two Streamers and two Collectors, then calls the live API to validate that telemetry reaches persistence and can be queried.

~~~bash
make test-e2e
~~~

The target stops the Compose services on exit. The Compose database volume is preserved; remove it manually only when you intentionally want to reset test data.

### 11.5 Build and OpenAPI consistency

~~~bash
make build
make openapi
git diff -- api/openapi.json
~~~

Review any generated OpenAPI change and commit it with the API contract change.

### 11.6 Helm validation

~~~bash
make helm-lint
make helm-template
~~~

These commands require Helm. They validate the chart syntax and render Kubernetes manifests locally; they do not prove that the chart can connect to a real cloud database or that pods become healthy in a target cluster.

### 11.7 Kind-based Kubernetes deployment smoke test

The Kubernetes smoke test deploys the actual Helm chart into a disposable Kind cluster. It builds and loads the API, Streamer and Collector images, starts a temporary PostgreSQL workload, installs the chart, waits for all workloads, checks the API readiness/data flow and verifies that all three HPA resources are present with a maximum of 10 replicas. GitHub Actions runs this target as part of CI.

Requirements: Docker Engine, a Kind cluster named `kind`, kubectl, Helm, curl and jq. Run from the repository root:

~~~bash
make k8s-smoke
~~~

The script removes its Helm release and test namespace on exit. Set `K8S_SMOKE_KEEP_RESOURCES=true` to retain them for inspection. This test validates a real Kubernetes deployment in Kind; it is not a substitute for a target-cloud installation or a representative load test proving HPA scale-up and scale-down.

### 11.8 Full local validation sequence

~~~bash
make doctor
make bootstrap
make fmt
make vet
make lint
make test
make coverage
make test-postgres TEST_DATABASE_URL='postgres://gpu_telemetry:TEST_PASSWORD@127.0.0.1:5432/gpu_telemetry_test?sslmode=disable'
make test-e2e
make openapi
make build
make docker-build
make helm-lint
make helm-template
~~~

The PostgreSQL, Docker and Helm targets require their respective services/tools. If a dependency is not installed, run the checks that apply to your selected deployment mode and record the skipped infrastructure-dependent checks.

## 12. Makefile command reference

Run `make help` for the current target list.

| Command | Purpose |
|---|---|
| `make doctor` | Check development prerequisites |
| `make bootstrap` | Download dependencies and create local environment template |
| `make fmt` | Format Go files |
| `make vet` | Run Go vet |
| `make lint` | Run golangci-lint when installed, else Go vet |
| `make test` | Run race-enabled tests across all packages |
| `make test-unit` | Run core unit tests |
| `make test-postgres` | Run PostgreSQL repository integration tests |
| `make test-e2e` | Run Docker Compose live-pipeline E2E test |
| `make coverage` | Enforce 90% core statement coverage |
| `make coverage-html` | Generate HTML coverage report |
| `make openapi` | Generate OpenAPI JSON |
| `make build` | Build API, Streamer and Collector binaries |
| `make docker-build` | Build three Docker images |
| `make compose-up` / `make compose-down` | Start/stop local stack |
| `make helm-lint` / `make helm-template` | Validate/render Helm chart |
| `make k8s-smoke` | Deploy and verify the chart in a disposable Kind cluster |
| `make deploy-local` | Install/upgrade chart into current Kubernetes context |
| `make clean` | Remove generated binaries and coverage output |

## 13. Troubleshooting

| Symptom | Checks |
|---|---|
| `make bootstrap` cannot download dependencies | Check network/proxy access to the Go module proxy and retry `go mod download`. |
| API `/readyz` returns 503 | Verify PostgreSQL is running, DSN host/port/database/credentials are correct, and the application can reach the DB. |
| GPU list is empty | Check Streamer and Collector logs, CSV path/read permissions, queue status and whether the dataset has started processing. |
| Streamer reports missing CSV/header | Confirm `TELEMETRY_CSV_PATH` points to the supplied file and required CSV columns are present. |
| Multiple Streamers appear to process one file | Confirm they use the same database and identical `TELEMETRY_SOURCE_ID`; the cursor is shared through PostgreSQL. |
| Messages repeatedly retry or go dead-letter | Inspect `queue_messages.last_error`, attempts, DB constraints and Collector logs. |
| Docker image build cannot find source/data | Run Docker commands from the repository root and keep the Compose build context set to the repository root. |
| Helm pods cannot read DB credentials | Confirm the `gpu-telemetry-db` Secret exists in the same namespace and contains the `DATABASE_URL` key. |
| HPA shows unknown CPU metrics | Check Metrics Server and ensure resource requests are configured. |
| Cluster cannot pull the image | Confirm image repository/tag and image pull permissions for private GHCR packages. |

Useful queue inspection queries:

~~~sql
SELECT status, COUNT(*) FROM queue_messages GROUP BY status ORDER BY status;

SELECT id, attempts, consumer_id, lease_until, last_error, created_at
FROM queue_messages
WHERE status = 'dead'
ORDER BY created_at DESC
LIMIT 50;

SELECT source_id, next_offset, header_offset, updated_at
FROM source_cursors;
~~~

## 14. Security and production checklist

Before a production/cloud deployment:

- Replace all development passwords and use a managed secret store.
- Use TLS for PostgreSQL and restrict network access to the database.
- Use least-privilege database credentials and private networking.
- Configure backups and point-in-time recovery for PostgreSQL.
- Publish immutable image tags for releases instead of relying only on `latest`.
- Configure private registry pull credentials when packages are not public.
- Review resource requests/limits, HPA thresholds and cluster capacity.
- Configure ingress/TLS, authentication/authorization and network policies where required by the environment.
- Add operational alerts for queue depth/age, dead-letter count, retries, processing throughput, API latency and PostgreSQL availability.

The sample Compose credentials and local HTTP endpoints are for development only. The repository does not currently provision cloud infrastructure, managed PostgreSQL, ingress, a secrets manager or production monitoring.

## 15. CI/CD and additional documentation

The GitHub Actions workflow runs formatting checks, vet, race-enabled Go tests, PostgreSQL integration tests, the 90% coverage gate, OpenAPI freshness, Helm validation, binary builds, Docker Compose E2E and the Kind-based Kubernetes deployment smoke test. On successful pushes to `main`, it also builds and publishes the API, Streamer and Collector images to GHCR.

Additional project documentation:

- [Architecture](docs/ARCHITECTURE.md)
- [Operations runbook](docs/OPERATIONS.md)
- [Validation status and environment-dependent checks](docs/VALIDATION.md)
- [AI assistance disclosure](docs/AI_USAGE.md)
- [Git setup](docs/GIT_SETUP.md)

Update the AI disclosure and validation record with the actual manual changes and test/build results from the environment used for project submission.

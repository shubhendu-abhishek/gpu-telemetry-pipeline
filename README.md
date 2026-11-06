# Elastic GPU Telemetry Pipeline with Custom Message Queue

A Golang pipeline that reads independent DCGM telemetry datapoints from CSV, publishes them to a custom durable queue, persists them in PostgreSQL, and exposes a REST API. The queue is implemented in Go and PostgreSQL; **Kafka, RabbitMQ, ZeroMQ and other external brokers are not used**.

## Assignment coverage

| Requirement | Implementation |
|---|---|
| CSV Streamer | `cmd/streamer`, `internal/streamer`, `internal/telemetry` |
| Scalable Streamers (max 10) | Shared transactional byte-offset cursor; Helm HPA |
| Custom message queue | PostgreSQL-backed queue: claim, lease, ack, retry and dead-letter |
| Scalable Collectors (max 10) | `FOR UPDATE SKIP LOCKED`; Helm HPA |
| API Gateway | `cmd/api`, `internal/api` |
| List GPUs | `GET /api/v1/gpus` |
| GPU telemetry query | `GET /api/v1/gpus/{id}/telemetry` |
| Inclusive time filter | `start_time >=`, `end_time <=`, on `processed_at` |
| Auto-generated OpenAPI | `make openapi`; `api/openapi.json` |
| Deployment | Docker, Compose, Kubernetes and Helm |
| Tests | Unit tests, PostgreSQL integration, Docker E2E; `make coverage` enforces 90% |

## Architecture

```mermaid
flowchart LR
 CSV[(CSV)] --> S[Streamer replicas]
 S --> Q[(Custom queue in PostgreSQL)]
 Q --> C[Collector replicas]
 C --> DB[(Telemetry + GPU inventory)]
 DB --> API[REST API Gateway]
 API --> Client[API client / Swagger]
```

Design decisions:
- `uuid` is the global GPU identity; `gpu_id` is a host-local index and may repeat.
- Preserve the CSV timestamp as `source_timestamp`; Streamer sets `streamed_at`; Collector sets UTC `processed_at`. API ordering and inclusive time filters use `processed_at` per the assignment.
- Queue delivery is at-least-once. Message leases support crash recovery; unique `message_id` makes telemetry persistence idempotent.
- Streamer cursor advancement and queue insertion occur in one DB transaction, preventing skipped rows after rollback.
- Collector replicas claim disjoint work using `FOR UPDATE SKIP LOCKED`; failures retry with exponential backoff and eventually become inspectable dead-letter rows.

## Repository layout

- `cmd/{api,streamer,collector,openapi-gen}` – runnable processes and OpenAPI generator
- `internal/{api,collector,config,postgres,queue,streamer,telemetry}` – application packages
- `db/migrations`, `internal/postgres/migrations` – PostgreSQL schema
- `api/openapi.json` – generated API contract
- `deployments/docker`, `deployments/docker-compose.yaml` – packaging/local stack
- `deployments/helm/gpu-telemetry` – Helm chart
- `tests/e2e` – black-box pipeline test
- `.github/workflows/ci.yaml` – CI, coverage, integration, E2E and image build
- `docs` – architecture, operations, AI disclosure, validation and Git workflow

## Prerequisites

For an Ubuntu 22.04/24.04 workstation or local VM:

| Tool | Requirement |
|---|---|
| Go | 1.26+ recommended; CI/container builder uses 1.27.3 |
| Git | 2.40+ |
| GNU Make | 4.x |
| Docker Engine | 25+ for Compose/E2E/image build |
| Docker Compose | v2 plugin |
| PostgreSQL | 15+ for manual local setup; Compose supplies PostgreSQL 16 |
| Helm | 4.3.1 |
| kubectl | Compatible with target cluster |
| Kubernetes | Managed/provider-supported version; Metrics Server needed for CPU HPA |

No physical GPU, CUDA toolkit or NVIDIA driver is required; CSV simulates telemetry. For cloud, prepare a managed Kubernetes cluster (EKS/GKE/AKS), reachable PostgreSQL, private networking, TLS, a container registry, Kubernetes Secret/secret manager, Metrics Server, backups and monitoring. Helm does not provision the database or cluster.

Check tools with `make doctor`.

## Quick start – Docker Compose

The sample CSV must be present at `data/dcgm_metrics_20250718_134233.csv`.

```bash
git clone https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline.git
cd gpu-telemetry-pipeline
make doctor
cp .env.example .env   # optional; Compose has development defaults
make compose-up
curl -fsS http://localhost:8080/readyz
# Swagger UI: http://localhost:8080/swagger
# OpenAPI:    http://localhost:8080/openapi.json
make compose-down
```

Scale local workers:

```bash
docker compose -f deployments/docker-compose.yaml up -d --scale streamer=3 --scale collector=4
```

`make compose-down` preserves the database volume. `docker compose -f deployments/docker-compose.yaml down -v` deletes it.

## Run on a VM without Docker

1. Install Go, Git, Make and PostgreSQL 15+.
2. Create a DB/user, for example:

```sql
CREATE USER gpu_telemetry WITH PASSWORD 'change-this-password';
CREATE DATABASE gpu_telemetry OWNER gpu_telemetry;
```

3. Clone, download dependencies, and configure environment:

```bash
git clone https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline.git
cd gpu-telemetry-pipeline
go mod download
export DATABASE_URL='postgres://gpu_telemetry:change-this-password@127.0.0.1:5432/gpu_telemetry?sslmode=disable'
export TELEMETRY_CSV_PATH="$PWD/data/dcgm_metrics_20250718_134233.csv"
export TELEMETRY_SOURCE_ID=dcgm-metrics-sample
```

For remote/cloud PostgreSQL, use TLS (`sslmode=require` or stronger) and private networking. Run each process in a separate terminal:

```bash
go run ./cmd/api
go run ./cmd/streamer
go run ./cmd/collector
```

All processes apply idempotent schema migrations at startup.

## REST API

**List GPUs**

```bash
curl -s http://localhost:8080/api/v1/gpus | jq
```

**Query telemetry by globally unique GPU UUID**

```bash
GPU_UUID='GPU-...'
curl -s "http://localhost:8080/api/v1/gpus/${GPU_UUID}/telemetry" | jq
```

**Inclusive time window and pagination**

```bash
curl -s 'http://localhost:8080/api/v1/gpus/GPU-.../telemetry?start_time=2026-11-06T17:00:00Z&end_time=2026-11-06T18:00:00Z&limit=100&offset=0' | jq
```

Time parameters are RFC3339. `start_time` and `end_time` are inclusive. `limit` defaults to 1000 (max 5000); `offset` defaults to 0. Results order by `processed_at`, then database ID.

Operational endpoints: `/healthz`, `/readyz`, `/metrics`, `/openapi.json`, `/swagger`.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | local Compose DSN | PostgreSQL DSN |
| `HTTP_PORT` | `8080` | API port |
| `TELEMETRY_CSV_PATH` | `./data/dcgm_metrics_20250718_134233.csv` | Streamer input file |
| `TELEMETRY_SOURCE_ID` | `dcgm-metrics-sample` | Stable cursor key; change for a new file |
| `STREAMER_INTERVAL` | `500ms` | Delay between CSV datapoints |
| `COLLECTOR_WORKERS` | `4` | Goroutines per Collector pod (1–10) |
| `COLLECTOR_BATCH_SIZE` | `25` | Max messages per claim |
| `COLLECTOR_POLL_INTERVAL` | `500ms` | Empty/error polling delay |
| `QUEUE_LEASE_DURATION` | `60s` | Visibility lease |
| `QUEUE_MAX_ATTEMPTS` | `10` | Retry limit before dead-letter |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Tests, coverage and OpenAPI

```bash
make fmt
make vet
make lint
make test-unit
make coverage       # fails if core-package statement coverage < 90%
make coverage-html
TEST_DATABASE_URL='postgres://gpu_telemetry:gpu_telemetry_test@localhost:5432/gpu_telemetry?sslmode=disable' make test-postgres
make test-e2e       # Docker Compose + live HTTP flow
make openapi        # regenerate api/openapi.json
```

Core coverage includes API, config, telemetry/CSV, Streamer and Collector. PostgreSQL repository tests require a live database; E2E requires Docker. The E2E test validates inventory, telemetry, timestamps and an inclusive boundary query.

## Docker and Kubernetes

```bash
make docker-build
make compose-up
make helm-lint
make helm-template
```

For Kubernetes, first build/push API, Streamer and Collector images to a reachable registry. Create a namespace and DB Secret (use a real secret manager in cloud):

```bash
kubectl create namespace gpu-telemetry
kubectl -n gpu-telemetry create secret generic gpu-telemetry-db \
  --from-literal=DATABASE_URL='postgres://USER:PASSWORD@DB_HOST:5432/DB_NAME?sslmode=require'
helm upgrade --install gpu-telemetry deployments/helm/gpu-telemetry \
  --namespace gpu-telemetry --create-namespace
kubectl -n gpu-telemetry get pods,svc,hpa,pdb
```

The chart defaults to 2 API, 2 Streamer and 2 Collector replicas; HPA can scale components up to 10. Metrics Server and CPU requests are required for CPU-based HPA. Configure image tags/repository in Helm values. Use an existing PVC if supplying a custom CSV.

## CI/CD and docs

GitHub Actions run formatting, vet, race-enabled tests, PostgreSQL integration, 90% coverage, OpenAPI freshness, Helm validation, binary builds and Docker Compose E2E. After successful CI on `main`, container images are published to GHCR.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/OPERATIONS.md](docs/OPERATIONS.md), [docs/AI_USAGE.md](docs/AI_USAGE.md), [docs/VALIDATION.md](docs/VALIDATION.md) and [docs/GIT_SETUP.md](docs/GIT_SETUP.md). Update the AI disclosure with actual manual corrections and verification before submission.

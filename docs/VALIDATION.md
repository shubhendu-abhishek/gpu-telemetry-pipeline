# Validation status and submission checklist

Last reviewed: 2026-11-09. This document distinguishes implemented functionality, automated checks that passed on the PR candidate, and infrastructure operations that have not been performed.

## Requirement-by-requirement status

| # | PDF requirement | Implementation location | Current evidence / status |
|---:|---|---|---|
| 1 | Telemetry Streamer reads CSV | `cmd/streamer`, `internal/streamer`, `internal/telemetry` | Implemented; parser and Streamer tests pass |
| 2 | Periodic telemetry publishing | `STREAMER_INTERVAL`, Streamer loop | Implemented; Compose E2E passes |
| 3 | Continuous CSV loop | Shared source cursor resets to header offset after EOF | Implemented; Streamer test suite passes |
| 4 | One CSV row is one datapoint | CSV parser and queue payload | Implemented; PostgreSQL queue integration and E2E pass |
| 5 | Processing time is telemetry timestamp | Collector assigns UTC `processed_at`; API filters this field | Implemented; E2E validates source/stream/process timestamps and inclusive boundary query |
| 6 | Custom message queue | Go queue interfaces and PostgreSQL queue in `internal/postgres/store.go` | Implemented; live PostgreSQL integration tests pass |
| 7 | No Kafka/RabbitMQ/ZeroMQ | No external message broker dependency | Compliant; no external broker is used |
| 8 | Queue scale/availability | Transactional cursor, `FOR UPDATE SKIP LOCKED`, lease expiry, Ack/Nack, retry/dead-letter | Verified: concurrent Collector claim test proves disjoint message claims; lease-expiry test proves another Collector can reclaim and the old owner cannot Ack; retry/dead-letter test passes |
| 9 | Telemetry Collector | `cmd/collector`, `internal/collector` | Implemented; race-enabled tests and multi-replica E2E pass |
| 10 | Collector scales dynamically (max 10) | Helm Collector Deployment and HPA | HPA template renders and clamps maxReplicas to 10; live HPA scale-up/down in a Kubernetes cluster is not performed |
| 11 | Streamer scales dynamically (max 10) | Helm Streamer Deployment and HPA; shared database cursor | HPA template renders and clamps maxReplicas to 10; two Streamer replicas pass Compose E2E; live Kubernetes HPA scaling is not performed |
| 12 | REST API Gateway | `cmd/api`, `internal/api` | Implemented; API tests and black-box E2E pass |
| 13 | OpenAPI generated automatically | `cmd/openapi-gen`, `make openapi`, `api/openapi.json` | Verified: generator runs and CI confirms the generated document is committed and current |
| 14 | Docker packaging | `deployments/docker/Dockerfile` and Compose | Verified: Compose E2E builds and runs API, Streamer, Collector and PostgreSQL images |
| 15 | Kubernetes/Helm deployment | `deployments/helm/gpu-telemetry` | Verified: `helm lint` and `helm template` pass; installation against a live Kubernetes cluster is not performed |
| 16 | Unit tests | API, config, telemetry, Streamer, Collector and PostgreSQL tests | Verified: `go test -race -count=1 ./...` passes, including PostgreSQL integration tests |
| 17 | System/E2E tests (bonus) | `tests/e2e/pipeline_test.go`, `make test-e2e` | Verified: Docker Compose E2E passes with two Streamer and two Collector replicas |
| 18 | Comprehensive README | `README.md` | README covers architecture, prerequisites, VM/local setup, Compose, Docker, Helm/cloud prerequisites, sample API workflow and operations; infrastructure-specific commands remain environment-dependent |
| 19 | Detailed AI-use documentation | `docs/AI_USAGE.md` | Expanded with two retained verbatim prompts, clearly labelled summaries of follow-up prompt intents, AI/manual split, manual interventions and limitations |
| 20 | Makefile coverage measurement | `make coverage`, `COVERAGE_PACKAGES`, `COVERAGE_THRESHOLD` | Verified: prints per-function and aggregate report; gate passed at **93.9%** against a 90% minimum for the five core packages |

## CI evidence for the current code candidate

- Pull request: [PR #5](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/pull/5)
- Code candidate tested: `c28fbe7ec4c4fbfc48d6390b996cf54c429234c1`
- GitHub Actions: [Run #13](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/actions/runs/42620809286)
- Result: both the main validation job and Docker Compose E2E job completed successfully.
- Successful main-job steps: dependency download, gofmt check, `go vet`, race-enabled unit and PostgreSQL integration tests, 90% coverage gate, OpenAPI generation/diff, Helm lint/template and application binary builds.
- Successful E2E step: `make test-e2e` launched the Compose environment with two Streamers and two Collectors and the black-box API pipeline test passed.
- The `container-images` publishing job is push-to-`main` only, so it is intentionally skipped for a pull-request run. The Compose E2E job builds the images locally; registry publishing has not been exercised by this PR.

## Coverage scope and measured result

The 90% coverage gate applies to these five core application packages:

- `./internal/api`: **91.4%**
- `./internal/config`: **94.2%**
- `./internal/telemetry`: **95.5%**
- `./internal/streamer`: **94.4%**
- `./internal/collector`: **95.9%**
- Aggregate reported by the Makefile: **93.9%** (minimum: **90.0%**)

The Makefile runs race-enabled tests with atomic coverage instrumentation, writes `coverage.out`, prints the per-function and total report through `go tool cover -func`, and fails below `COVERAGE_THRESHOLD` (default 90%). This is **not a 90% whole-repository claim**. PostgreSQL is validated through separate live integration tests, including concurrency and expired-lease recovery; the postgres package is not part of the five-package coverage aggregate.

Run locally:

```bash
make coverage
make coverage-html
```

## Validation commands

The following sequence is configured in CI and/or documented for a provisioned environment:

```bash
gofmt -w $(find cmd internal tests -name '*.go' -type f)
go mod tidy
go vet ./...
go test -race -count=1 ./...
make coverage
make openapi
git diff --exit-code -- api/openapi.json
make build
make helm-lint
make helm-template
make test-postgres TEST_DATABASE_URL='<disposable test database DSN>'
make docker-build
make test-e2e
```

## Remaining environment-dependent work

The code, automated test suite, OpenAPI consistency, Helm rendering and Compose E2E have passed on the recorded PR candidate. These are the remaining tasks outside the current GitHub Actions verification:

1. **Live Kubernetes smoke test:** install the chart into a disposable cluster/namespace, verify pods and Services become ready, then observe HPA scaling with Metrics Server. The CI job currently validates chart syntax/rendering, not a live cluster.
2. **Cloud provisioning:** provision an actual Kubernetes cluster, managed PostgreSQL, registry access, networking, TLS, secrets manager, backups, ingress and monitoring in the target cloud account. The repo does not create those resources.
3. **Registry publishing:** the workflow publishes images on a push to `main`; the PR run intentionally does not execute that job.

The Helm chart deploys the application workloads only. PostgreSQL is external and must be provisioned separately. The cluster must be able to reach the database, the database URL must be supplied through the chart's expected Secret, and Metrics Server is required for CPU-based HPA.

## Current PR record

- Candidate branch: `fix/ci-quality-gaps`
- Base branch: `main`
- PR: #5 (open; awaiting review/merge)
- Code tested by Run #13: `c28fbe7ec4c4fbfc48d6390b996cf54c429234c1`
- Validation documentation is updated after that successful run; its documentation-only commit is part of PR #5 and will receive its own PR workflow run.
- Cloud deployment and live Kubernetes HPA smoke test: not performed.

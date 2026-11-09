# Validation status and submission checklist

Last reviewed: 2026-11-09. This record distinguishes implemented functionality, automated evidence from GitHub Actions, and infrastructure operations that still require a target environment.

## Requirement-by-requirement status

| # | PDF requirement | Implementation location | Current evidence / status |
|---:|---|---|---|
| 1 | Telemetry Streamer reads CSV | `cmd/streamer`, `internal/streamer`, `internal/telemetry` | Implemented; parser and Streamer tests pass |
| 2 | Periodic telemetry publishing | `STREAMER_INTERVAL`, Streamer loop | Implemented; Docker Compose E2E passes |
| 3 | Continuous CSV loop | Shared source cursor resets to header offset after EOF | Implemented; Streamer tests pass |
| 4 | One CSV row is one datapoint | CSV parser and queue payload | Implemented; PostgreSQL integration and E2E tests pass |
| 5 | Processing time is telemetry timestamp | Collector assigns UTC `processed_at`; API filters this field | Implemented; E2E validates processing timestamps and inclusive time filters |
| 6 | Custom message queue | Go queue interfaces and PostgreSQL queue in `internal/postgres/store.go` | Implemented; live PostgreSQL integration tests pass |
| 7 | No Kafka/RabbitMQ/ZeroMQ | No external message broker dependency | Compliant; no external broker is used |
| 8 | Queue scale/availability | Transactional cursor, `FOR UPDATE SKIP LOCKED`, leases, Ack/Nack, retry/dead-letter | Verified by concurrent Collector claims, lease-expiry recovery, old-owner Ack rejection and retry/dead-letter integration tests |
| 9 | Telemetry Collector | `cmd/collector`, `internal/collector` | Implemented; race-enabled tests and multi-replica Compose E2E pass |
| 10 | Collector scales dynamically (max 10) | Helm Collector Deployment and HPA | HPA template caps `maxReplicas` at 10; multi-replica Compose run passes; real HPA load-driven scale-up/down still needs a measured cluster test |
| 11 | Streamer scales dynamically (max 10) | Helm Streamer Deployment and HPA; shared database cursor | HPA template caps `maxReplicas` at 10; two Streamer replicas pass Compose E2E; real HPA load-driven scale-up/down still needs a measured cluster test |
| 12 | REST API Gateway | `cmd/api`, `internal/api` | Implemented; API tests and black-box E2E pass |
| 13 | OpenAPI generated automatically | `cmd/openapi-gen`, `make openapi`, `api/openapi.json` | Verified: generator runs and CI checks that the committed document is current |
| 14 | Docker packaging | `deployments/docker/Dockerfile` and Compose | Verified: Compose E2E builds and runs API, Streamer, Collector and PostgreSQL images |
| 15 | Kubernetes/Helm deployment | `deployments/helm/gpu-telemetry` | Helm lint/render pass. A Kind deployment smoke test is now part of CI on this branch; its result must be recorded after the PR workflow completes. A real cloud-cluster install remains environment-specific |
| 16 | Unit tests | API, config, telemetry, Streamer, Collector and PostgreSQL tests | Verified: `go test -race -count=1 ./...` passes, including PostgreSQL integration tests |
| 17 | System/E2E tests (bonus) | `tests/e2e/pipeline_test.go`, `make test-e2e` | Verified: Docker Compose E2E passes with two Streamer and two Collector replicas |
| 18 | Comprehensive README | `README.md` | Covers architecture, prerequisites, VM/local setup, Compose, Docker, Helm/cloud setup, sample API workflow and operations |
| 19 | Detailed AI-use documentation | `docs/AI_USAGE.md` | Expanded with two retained verbatim prompts, clearly labelled summaries of follow-up prompt intents, AI/manual split, manual interventions and limitations. The full original prompt transcript was not retained |
| 20 | Makefile coverage measurement | `make coverage`, `COVERAGE_PACKAGES`, `COVERAGE_THRESHOLD` | Verified on the latest main run: **93.9%** aggregate for the five core packages, above the 90% threshold |

## Latest successful main-branch CI evidence

- Main branch commit: [`701a8f26727cd2198347f8f7ab5d66c7cca36140`](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/commit/701a8f26727cd2198347f8f7ab5d66c7cca36140)
- Pull request #6 (AI-use disclosure): [merged](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/pull/6)
- GitHub Actions main push run: [Run 42624642658](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/actions/runs/42624642658)
- Result: all three jobs completed successfully:
  - `test`: formatting, `go vet`, race-enabled unit/integration tests, 90% coverage gate, OpenAPI consistency, Helm lint/template and binary builds.
  - `end-to-end`: Docker Compose pipeline E2E.
  - `container-images`: API, Streamer and Collector images all built and pushed to GHCR successfully.
- This closes the earlier registry-publishing evidence gap for the current `main` commit. It does not prove that a separately provisioned cluster can pull and run the images.

## Coverage scope and measured result

The Makefile's 90% coverage gate applies to these five core application packages:

- `./internal/api`: **91.4%**
- `./internal/config`: **94.2%**
- `./internal/telemetry`: **95.5%**
- `./internal/streamer`: **94.4%**
- `./internal/collector`: **95.9%**
- Aggregate reported by the Makefile: **93.9%** (minimum: **90.0%**)

This is not a 90% whole-repository claim. PostgreSQL behavior is exercised through separate live integration tests, including concurrency and expired-lease recovery; the PostgreSQL adapter is not part of the five-package coverage aggregate.

Run locally:

```bash
make coverage
make coverage-html
```

## Validation commands

The automated test workflow runs the Go, PostgreSQL, coverage, OpenAPI, Helm and binary checks. It also runs Docker Compose E2E and, on the current branch, a Kind-based Kubernetes deployment smoke test.

```bash
gofmt -w $(find cmd internal tests -name '*.go' -type f)
go mod tidy
go vet ./...
go test -race -count=1 ./...
make coverage
make openapi
git diff --exit-code -- api/openapi.json
make build
make helm-lint helm-template
make test-postgres TEST_DATABASE_URL='<disposable test database DSN>'
make test-e2e
make k8s-smoke
```

`make k8s-smoke` is destructive only within its dedicated smoke-test namespace. It requires a disposable Kind cluster named `kind`, Docker, kubectl, Helm, curl and jq. The script creates a temporary PostgreSQL workload, loads locally built application images into Kind, installs the chart, checks workload readiness and API data flow, checks all three HPA resources and verifies the configured maximum is 10. It deletes the smoke namespace and Helm release on exit unless `K8S_SMOKE_KEEP_RESOURCES=true` is set.

## Remaining environment-dependent work

1. **Kind CI result:** the new Kubernetes smoke job has been added on this branch. Do not mark the new job passed until its PR workflow is green.
2. **HPA load-driven scaling:** the chart defines CPU-based HPAs with a maximum of 10 and the Kind smoke checks the live HPA objects. A representative load test that proves scale-up and scale-down should still be executed and recorded; it is not claimed as passed by the current smoke test.
3. **Target cloud deployment:** provision the actual Kubernetes cluster, managed PostgreSQL, registry access, networking, TLS, secrets manager, backups and monitoring in the intended cloud account. The repository does not create those resources.
4. **AI prompt transcript:** only two original prompts are retained verbatim in `docs/AI_USAGE.md`; the remaining follow-up instructions are accurately labelled summaries. Do not invent missing verbatim prompts.

The Helm chart deploys the three application workloads only. PostgreSQL is external to the chart and must be reachable from the cluster. The database URL is supplied through the chart's expected Secret. Metrics Server is required for CPU-based HPA operation.

## AI assistance disclosure

See [`docs/AI_USAGE.md`](AI_USAGE.md). It describes the tools used, phase-by-phase AI/manual work split, retained verbatim prompts, reconstructed prompt summaries, human decisions, validation evidence and limitations. The missing historical prompt wording cannot be reconstructed reliably from the current repository state.

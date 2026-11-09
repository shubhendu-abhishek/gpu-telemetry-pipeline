# Validation status and submission checklist

Last reviewed: 2026-11-09. This document records the current merged `main` evidence separately from changes proposed in the Makefile/CI alignment pull request.

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
| 10 | Collector scales dynamically (max 10) | Helm Collector Deployment and HPA | Live HPA resource deployed in Kind; test checks the maximum is 10. Load-driven scale-up and scale-down have not been measured |
| 11 | Streamer scales dynamically (max 10) | Helm Streamer Deployment and HPA; shared database cursor | Live HPA resource deployed in Kind; test checks the maximum is 10. Load-driven scale-up and scale-down have not been measured |
| 12 | REST API Gateway | `cmd/api`, `internal/api` | Implemented; API tests and black-box E2E pass |
| 13 | OpenAPI generated automatically | `cmd/openapi-gen`, `make openapi`, `api/openapi.json` | Verified: generator runs and CI checks that the committed document is current |
| 14 | Docker packaging | `deployments/docker/Dockerfile` and Compose | Verified: Compose E2E builds and runs API, Streamer, Collector and PostgreSQL images |
| 15 | Kubernetes/Helm deployment | `deployments/helm/gpu-telemetry`, `scripts/k8s-smoke.sh` | Verified in a disposable Kind cluster: PostgreSQL, Helm release, API/Streamer/Collector rollouts, API readiness, telemetry visibility and three HPA caps |
| 16 | Unit tests | API, config, telemetry, Streamer, Collector and PostgreSQL tests | Verified: `go test -race -count=1 ./...` passes with PostgreSQL service in CI |
| 17 | System/E2E tests (bonus) | `tests/e2e/pipeline_test.go`, `make test-e2e` | Verified: Docker Compose E2E passes with two Streamer and two Collector replicas |
| 18 | Comprehensive README | `README.md` | Covers architecture, prerequisites, VM/local setup, Compose, Docker, Helm/cloud setup, sample API workflow and operations |
| 19 | Detailed AI-use documentation | `docs/AI_USAGE.md` | Two original prompts are retained verbatim. Follow-up prompt intents are labelled as summaries; the complete historical prompt transcript was not retained |
| 20 | Makefile coverage measurement | `make coverage`, `COVERAGE_PACKAGES`, `COVERAGE_THRESHOLD` | Verified on merged `main`: **93.9%** aggregate for the five core packages, above the 90% threshold |

## Latest merged main-branch CI evidence

- Main branch commit: [`ff9505aea76ffa8b44788416ac2d39126d5fbac2`](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/commit/ff9505aea76ffa8b44788416ac2d39126d5fbac2)
- Pull request #7: [merged](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/pull/7)
- GitHub Actions main push run: [Run 42633802871](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/actions/runs/42633802871)
- Result: **all four jobs completed successfully**:
  - `test`: formatting, `go vet`, race-enabled unit/integration tests, 90% coverage gate, OpenAPI freshness, Helm lint/render and binary builds.
  - `end-to-end`: Docker Compose pipeline E2E.
  - `kubernetes-smoke`: Kind cluster creation, temporary PostgreSQL deployment, Helm install, all three application Deployment rollouts, API readiness, telemetry visibility and HPA maximum checks.
  - `container-images`: API, Streamer and Collector images built and pushed to GHCR.
- The Kind smoke-test log reports: `Kind deployment smoke test passed: API, Streamer, Collector, PostgreSQL, telemetry flow and all three HPA caps were verified.`
- Coverage log: **93.9%**, passing the **90.0%** gate.
- This verifies the container registry publishing workflow and an application deployment to Kind. It does not mean that a target cloud account has been provisioned or that HPA scaling has been demonstrated under a representative load.

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

## Makefile target integration

The Makefile includes these categories:

| Category | Targets | Intended use |
|---|---|---|
| Setup and help | `help`, `doctor`, `bootstrap` | Discover targets, check tools, download dependencies and create local environment template |
| Go quality | `fmt`, `fmt-check`, `vet`, `lint` | Format code, check formatting without edits, vet and run golangci-lint if installed |
| Tests | `test`, `test-unit`, `test-postgres`, `test-e2e` | Race-enabled tests, core-package unit tests, PostgreSQL integration and black-box Compose E2E |
| Coverage | `coverage`, `coverage-html` | Enforce 90% core-package coverage and produce an HTML report |
| API/build | `openapi`, `build` | Generate OpenAPI and build the three Go binaries |
| Containers | `docker-build`, `compose-up`, `compose-down` | Build images and manage the local Compose stack |
| Kubernetes | `helm-lint`, `helm-template`, `k8s-smoke`, `deploy-local` | Validate/render chart, deploy/test in disposable Kind, or install into a selected cluster |
| Cleanup | `clean` | Remove generated binaries and coverage output |

The Makefile/CI alignment change is proposed in a follow-up PR. It introduces `make fmt-check` and a consolidated `make verify` target so CI and local development can invoke the same non-container validation sequence. The existing merged-main CI already invokes the Makefile targets for coverage, OpenAPI, Helm, binary build, Compose E2E and Kind smoke; the alignment PR consolidates the remaining format, vet, lint and Go test commands under `make verify`.

## Validation commands

On the merged `main` branch, the following sequence is supported:

```bash
make doctor
make bootstrap
make fmt
make fmt-check
make vet
make lint
make test
make coverage
make test-postgres TEST_DATABASE_URL='<disposable test database DSN>'
make test-e2e
make openapi
git diff --exit-code -- api/openapi.json
make build
make docker-build
make helm-lint
make helm-template
make k8s-smoke
```

Important: `make test-postgres` needs a dedicated disposable PostgreSQL database. The CI Go test job supplies `TEST_DATABASE_URL` and runs `go test -race -count=1 ./...`, which includes the PostgreSQL adapter tests. `make test-e2e` requires Docker Compose. `make k8s-smoke` requires a disposable Kind cluster named `kind`, Docker, kubectl, Helm, curl and jq.

## Remaining environment-dependent work

1. **HPA performance test:** the chart and live HPA objects are validated, including a max of 10. A representative load test proving actual scale-up and scale-down, with observed replica counts and metrics, is not yet recorded.
2. **Target cloud deployment:** deploy to the intended EKS/GKE/AKS or other cluster and validate managed PostgreSQL connectivity, registry access, networking, TLS, secrets, backups, ingress and monitoring. Kind is a local Kubernetes test cluster, not a cloud deployment.
3. **AI prompt transcript:** only two original prompts are retained verbatim in `docs/AI_USAGE.md`; the remaining follow-up instructions are honestly labelled summaries. Do not invent missing verbatim prompts.

The Helm chart deploys the three application workloads only. PostgreSQL is external to the chart. The database URL is supplied through the chart's expected Secret. Metrics Server is required for CPU-based HPA operation.

## AI assistance disclosure

See [`docs/AI_USAGE.md`](AI_USAGE.md). It describes tools used, phase-by-phase AI/manual work, retained verbatim prompts, reconstructed prompt summaries, human decisions, validation evidence and limitations.

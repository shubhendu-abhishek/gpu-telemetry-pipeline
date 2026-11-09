# Validation status and submission checklist

Last reviewed: 2026-11-09. This document separates source-level implementation from checks that require a successful build or provisioned infrastructure. Update the commit SHA, workflow URL and measured coverage after each release candidate.

## Requirement-by-requirement status

| # | PDF requirement | Implementation location | Status / evidence needed |
|---:|---|---|---|
| 1 | Telemetry Streamer reads CSV | cmd/streamer, internal/streamer, internal/telemetry | Implemented; exercise with supplied CSV |
| 2 | Periodic telemetry publishing | STREAMER_INTERVAL, Streamer loop | Implemented; verify configured rate under load |
| 3 | Continuous CSV loop | Shared source cursor resets to header offset after EOF | Implemented; verify repeated loop and restart behavior |
| 4 | One CSV row is one datapoint | CSV parser and queue payload | Implemented; verify row-to-message count |
| 5 | Processing time is telemetry timestamp | Collector assigns UTC processed_at; API filters this field | Implemented; validate E2E timestamp and inclusive boundaries |
| 6 | Custom message queue | Go queue interfaces and PostgreSQL queue in internal/postgres/store.go | Implemented; PostgreSQL integration and concurrency testing required |
| 7 | No Kafka/RabbitMQ/ZeroMQ | No external message broker dependency | Compliant |
| 8 | Queue scale/availability | Transactional cursor, FOR UPDATE SKIP LOCKED, lease expiry, Ack/Nack, retry/dead-letter | Designed/implemented; prove with multiple consumers, crash and lease-expiry tests |
| 9 | Telemetry Collector | cmd/collector, internal/collector | Implemented; run worker tests and E2E |
| 10 | Collector scales dynamically (max 10) | Helm Collector Deployment and HPA | Helm templates present; lint/render and cluster HPA test required |
| 11 | Streamer scales dynamically (max 10) | Helm Streamer Deployment and HPA; shared database cursor | Helm templates present; multi-replica cursor/cluster test required |
| 12 | REST API Gateway | cmd/api, internal/api | Implemented; run API tests and smoke requests |
| 13 | OpenAPI generated automatically | cmd/openapi-gen, make openapi, api/openapi.json | Generator/target present; CI freshness check must pass |
| 14 | Docker packaging | deployments/docker/Dockerfile and Compose | Present; build all three images and run Compose E2E |
| 15 | Kubernetes/Helm deployment | deployments/helm/gpu-telemetry | Chart present; Helm lint/template and a disposable cluster install required |
| 16 | Unit tests | API, config, telemetry, Streamer and Collector test packages | Present; CI race test must pass |
| 17 | System/E2E tests (bonus) | tests/e2e/pipeline_test.go, make test-e2e | Present; execute against the live Compose stack |
| 18 | Comprehensive README | README.md | Present; validate commands in a clean environment |
| 19 | Detailed AI-use documentation | docs/AI_USAGE.md | Expanded with retained prompts, representative prompt intents, AI/manual split and limitations |
| 20 | Makefile coverage measurement | make coverage, COVERAGE_PACKAGES, COVERAGE_THRESHOLD | Implemented: writes coverage.out, prints go tool cover -func output and fails below 90% for the five core packages; percentage must be captured from the actual CI run |

## Coverage scope

The 90% threshold is deliberately scoped to these core application packages:

- ./internal/api
- ./internal/config
- ./internal/telemetry
- ./internal/streamer
- ./internal/collector

The Makefile runs race-enabled tests with atomic coverage instrumentation, writes coverage.out, prints the function and total coverage report through go tool cover -func, and enforces COVERAGE_THRESHOLD (default 90%). It is **not** a 90% whole-repository claim. The PostgreSQL package has separate live integration tests and should be assessed with a real database; command entrypoints and infrastructure templates are also outside this five-package aggregate.

Run:

```bash
make coverage
make coverage-html
```

Record the actual displayed total here after the CI or local run. Do not carry forward previously recorded percentages unless the current source revision has produced them again.

## Required validation commands

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

The PostgreSQL integration test requires TEST_DATABASE_URL; Docker build/E2E requires Docker Engine and Compose v2; Helm checks require Helm. A successful Helm render is not a live Kubernetes deployment test.

## Known deployment boundary

The Helm chart deploys the API, Streamers and Collectors. PostgreSQL is external and must be provisioned separately. The target cluster must be able to reach that database, the database URL must be supplied through the chart's expected Secret, and Metrics Server is required for CPU-based HPA. The repository does not provision cloud infrastructure, managed PostgreSQL, ingress, DNS, certificates or an external secrets manager.

## Current candidate record

- Candidate branch: fix/ci-quality-gaps
- Base branch: main
- Main base commit at branch creation: 5a2d83fa69313b221d7a5527197dfe39952d0eb8
- Final candidate commit: pending CI repair/PR
- GitHub Actions URL and outcome: pending
- Measured core coverage: pending CI output
- PostgreSQL integration: pending CI result
- Docker Compose E2E: pending CI result
- Helm lint/template: pending CI result
- Kubernetes smoke test: not performed in this environment

Do not mark the candidate as submission-ready until the Actions run for the exact PR head is green and environment-dependent checks have a recorded outcome.

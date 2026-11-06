# Validation status

Validation record prepared on 2026-11-06. This file distinguishes locally checked behavior from tests requiring provisioned infrastructure.

## Locally passed / recorded

- `gofmt` applied to Go source and tests.
- Core-package coverage gate recorded at 93.9% aggregate: API 91.4%, config 94.2%, telemetry/CSV 95.5%, Streamer 94.4%, Collector 95.9%.
- Go packages compiled and race-enabled tests / `go vet` were checked using a temporary local pgx compile-interface stub because Go module download could not reach the proxy. This is not a substitute for compiling against the real pgx dependency.
- E2E test package compiles; the live test is gated behind `RUN_E2E=1`.
- Compose/CI/Helm YAML and OpenAPI JSON were statically inspected.

## Still requires a provisioned environment

- Live PostgreSQL integration: set `TEST_DATABASE_URL` and run `make test-postgres`.
- Docker image build and Compose E2E: install Docker Engine/Compose and run `make docker-build` and `make test-e2e`.
- Helm lint/template and Kubernetes install: install Helm and run `make helm-lint helm-template`, then perform a cluster smoke test.
- Verify image registry access, managed PostgreSQL TLS/backup, Kubernetes Secrets, Metrics Server and service networking in the intended cloud account.

Do not describe skipped/unavailable tests as passed. CI provisions PostgreSQL and runs the automated coverage, integration, E2E, image and Helm checks where configured.

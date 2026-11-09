# Repository upload status

The complete application source tree has been published to the `feature/gpu-telemetry-implementation` branch from the current `main` branch tip.

The branch contains the Golang API, Streamer and Collector; PostgreSQL-backed custom queue and migrations; original DCGM CSV dataset; unit, PostgreSQL integration and E2E tests; generated OpenAPI; Makefile; Docker/Compose; Helm chart; CI workflow; and architecture, setup, operations and AI-use documentation.

Review the feature branch and wait for CI validation before merging it into `main`. The live PostgreSQL integration tests and Docker/Helm environment checks must pass in CI or a provisioned environment; see `docs/VALIDATION.md` for the validation boundary.

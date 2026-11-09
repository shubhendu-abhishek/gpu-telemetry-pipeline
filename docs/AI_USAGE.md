# AI assistance disclosure

This document records how AI assistance was used for the GPU Telemetry Pipeline assignment. It distinguishes retained verbatim prompts from normalized descriptions of follow-up work; it does not claim that every iterative prompt was archived word-for-word.

## Tools and responsibility split

| Tool / person | Use |
|---|---|
| ChatGPT (GPT-6) | Requirement breakdown, architecture discussion, Go implementation drafts, test ideas, Docker/Compose and Helm scaffolding, documentation, and review checklists |
| GitHub-connected repository tools | Read repository files, create a repair branch, update documentation/workflows, and open reviewable changes |
| Go toolchain in CI / development environment | gofmt, go mod tidy, go vet, Go tests, race detector, coverage and binary builds |
| Docker, Docker Compose, Helm and Kubernetes | Runtime/package/deployment verification where those tools and infrastructure are available |
| Maintainer | Owns design decisions, source/PDF/CSV review, credentials and infrastructure, code review, test interpretation and submission decision |

AI generated suggestions and code drafts; it did not independently prove correctness or production readiness. The maintainer remains responsible for reviewing and running the software in the intended environment.

## Retained prompts

The following two prompts are retained verbatim from the project conversation:

**1. Assignment analysis and project scope**

> Can you go through the pdf file and csv file and write the steps as per given requirement to make project and help me to push into git. Need to completed all of the given feature for this project. like Make file README details step wise process, to setup the application as what are the prerequisites required before running application into any of the system. cloud as well local vm machine. Unit test 90% minimum e2e test.

**2. Implementation/bootstrap**

> as per the plan can you write the code and create a git project to push with all requirements

The later work included iterative requests to expand the README with prerequisites and ordered installation steps, support local VM/Docker Compose/cloud deployment guidance, review REST methods and timestamp behavior, and validate the repository against the PDF. The exact text of every follow-up was not retained; the descriptions below are summaries, not verbatim quotations.

## Development workflow and representative prompt intents

| Stage | AI-assisted work | Representative instruction / intent |
|---|---|---|
| Source analysis | Convert the PDF into a checklist; inspect the CSV fields, GPU identity and optional columns | Read the supplied PDF and CSV and identify every runtime, API, packaging, testing and documentation requirement |
| Architecture | Propose Go Streamer, custom PostgreSQL-backed queue, Collector, persistence and REST API boundaries | Design a scalable telemetry pipeline without Kafka, RabbitMQ or ZeroMQ, including coordination for at most 10 Streamers and Collectors |
| Queue and persistence | Draft queue interfaces, transactional CSV cursor, message lease, acknowledgement, retry and dead-letter flow | Implement a durable custom queue with safe concurrent claiming, restart recovery and idempotent telemetry persistence |
| API and contract | Draft GPU inventory and time-filtered telemetry endpoints and OpenAPI contract | Implement the two specified REST endpoints, inclusive processing-time filters, validation and generated OpenAPI JSON |
| Testing | Draft parser, configuration, Streamer, Collector, API, PostgreSQL integration and black-box E2E tests | Add unit tests with a 90% core-package coverage gate and a system/E2E path that exercises the running pipeline |
| Delivery | Draft Makefile, Dockerfile, Compose, Helm, CI and operator documentation | Provide build, local VM, Docker Compose, Helm and cloud setup instructions with repeatable validation commands |
| Audit | Compare source tree and workflow with the assignment checklist | Audit every PDF requirement and identify anything that is present in source but not yet verified by a successful run |

These descriptions summarize the actual work areas requested during the conversation. They should not be represented as an archived verbatim prompt transcript.

## What AI accelerated

- Turning the three-page assignment into an implementation/delivery checklist.
- Drafting Go component boundaries, queue contracts, API models and error-handling paths.
- Generating initial unit/integration/E2E test structures and coverage targets.
- Producing Docker, Compose, Helm and GitHub Actions scaffolding.
- Drafting architecture, README, operations and validation documentation.
- Reviewing implementation claims against the assignment and identifying verification gaps.

## Manual review and interventions

The following decisions and checks were explicitly reviewed because generated code or a simple first-pass interpretation could be incorrect:

1. **GPU identity:** use CSV UUID as globally unique identity; gpu_id / GPU index is host-local and may repeat across hosts.
2. **Timestamp semantics:** preserve the source CSV timestamp, add Streamer streamed_at, and set Collector UTC processed_at. API ordering and inclusive start_time / end_time filters use processed_at, as required by the assignment.
3. **Shared Streamer cursor:** cursor locking, queue insertion and offset advancement must be in one database transaction so scaled Streamers do not independently read/replay the same offset.
4. **Delivery semantics:** queue delivery is at-least-once. A lease allows a message to be reclaimed after a consumer crash; unique message IDs make persistence idempotent before acknowledgement.
5. **Collector scaling:** concurrent claims use PostgreSQL FOR UPDATE SKIP LOCKED; retry/backoff and dead-letter handling need to be checked against lease expiry and worker failures.
6. **CSV optional fields:** container, pod and namespace may be empty in the provided sample and must not be treated as mandatory.
7. **Pagination:** the telemetry endpoint returns bounded pages (limit/offset), ordered deterministically; callers retrieve all matching entries by following pages rather than relying on an unbounded response.
8. **Deployment boundary:** Helm deploys application workloads. The chart expects an external PostgreSQL database and a pre-created Secret; it does not provision a cloud cluster or managed database.
9. **Configuration:** Go services read environment variables; .env is a shell/Compose convenience and is not automatically loaded by the Go binaries.
10. **Coverage wording:** the Makefile's 90% gate applies to the five core logic packages listed in COVERAGE_PACKAGES, not every package in the repository. PostgreSQL integration tests are separately gated by TEST_DATABASE_URL.

## Build and test environment limitations

A successful code generation or documentation pass is not equivalent to a successful runtime test. Docker, Helm, a Kubernetes cluster and a live PostgreSQL instance must be available for their corresponding checks. When a local environment cannot access the Go module proxy, cannot download pgx, or does not have Docker/Helm installed, record the check as blocked/not run; do not describe a local stub compile or static inspection as proof of a real PostgreSQL, Docker or Kubernetes run.

GitHub Actions is configured to provide PostgreSQL for automated tests and to execute formatting, vet, race-enabled tests, core coverage, OpenAPI freshness, Helm lint/render, binary build and Docker Compose E2E checks. The actual workflow result for the submitted commit must be checked and linked in the final validation record.

## Human review checklist before submission

- [ ] Review the complete diff and ensure no generated or unrelated files were introduced.
- [ ] Confirm the Go code is formatted and go.sum is committed.
- [ ] Confirm go vet ./... and go test -race -count=1 ./... pass using the real pgx dependency.
- [ ] Confirm make coverage reports the measured percentage and passes the 90% core-package threshold.
- [ ] Run PostgreSQL integration tests against a disposable real database.
- [ ] Run Docker image builds and Docker Compose E2E.
- [ ] Run Helm lint/template and, separately, deploy to a disposable Kubernetes namespace for a smoke test.
- [ ] Regenerate OpenAPI and confirm api/openapi.json has no uncommitted diff.
- [ ] Update docs/VALIDATION.md with exact commit SHA, Actions run URL, test outcomes, coverage percentage and any skipped infrastructure checks.
- [ ] Remove temporary repair tooling used only to fix the repository; keep only the normal CI workflow.

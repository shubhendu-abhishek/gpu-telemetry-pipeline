# Documenting the use of AI

## 1. Purpose of this disclosure

AI assistance was used during analysis, design, implementation, testing, packaging and documentation of the GPU Telemetry Pipeline. This file explains where AI was used, the kinds of prompts and outputs involved, what work was accelerated, what required human judgement, where the first AI-assisted output was incomplete, and how the resulting implementation was checked.

The project is AI-assisted, not AI-verified. Generated code and documentation were integrated into the repository only after review and validation steps. The maintainer remains responsible for the technical decisions, final source, credentials, infrastructure and submission.

## 2. Tools used

| Tool | How it was used | Important distinction |
|---|---|---|
| OpenAI ChatGPT | Requirement analysis, architecture discussion, Go code and test drafts, Docker/Compose and Helm scaffolding, CI/documentation drafts, and iterative troubleshooting | The exact model/version for every historical development turn was not recorded. |
| GitHub repository integration | Read repository files, create branches, update files, create/review pull requests and inspect workflow results | This provided repository access; it is not an AI code generator. |
| Go toolchain | `gofmt`, `go mod tidy`, `go vet`, tests, race detector, coverage and binary builds | Deterministic build and validation tools, not AI. |
| GitHub Actions | Ran Go, PostgreSQL-backed, coverage, OpenAPI, Helm, binary-build and E2E checks | Automated CI evidence; it does not replace a production deployment. |
| Docker / Docker Compose / Helm | Built and ran the local multi-service application and validated/rendered Helm templates | Infrastructure and packaging tools, not AI. |

No AI model was given production credentials or access to a production GPU cluster. The implementation uses the supplied CSV as its telemetry input; it does not require access to a physical GPU for the assignment workflow.

## 3. How AI was used during development

| Development phase | AI-assisted activities | Human/maintainer activities |
|---|---|---|
| Requirement analysis | Turned the supplied PDF into a requirement checklist and helped interpret the supplied CSV columns and expected application flow | Supplied the assignment and dataset; checked the checklist against the PDF and dataset; retained the assignment constraints, including the prohibition on external brokers |
| Repository bootstrap | Suggested the Go project structure, component boundaries, Makefile targets and documentation layout | Created/owned the GitHub repository and reviewed the proposed tree and deliverables |
| Architecture and design | Drafted the Streamer → custom queue → Collector → PostgreSQL → REST API design and discussed scale/reliability approaches | Evaluated the design against the assignment, selected the PostgreSQL-backed custom queue, and reviewed data identity, transaction and delivery semantics |
| Code bootstrap | Assisted with drafts for CSV parsing, Streamer, Collector, queue interfaces, PostgreSQL persistence, API handlers, configuration and OpenAPI generation | Integrated and reviewed the code, checked interfaces and error paths, and used CI/test feedback to correct issues |
| Unit and integration tests | Suggested test cases and drafted tests for parsing, configuration, API behavior, Streamer, Collector and PostgreSQL queue behavior | Checked test assertions and added/reviewed regression coverage for concurrent claims, expired leases, retries, dead-letter behavior and API timestamp filtering |
| Build and deployment environment | Drafted Dockerfile, Compose, Helm, GitHub Actions and local VM/cloud setup instructions | Validated Docker Compose E2E, Helm lint/render and binary builds in CI; documented that live Kubernetes/cloud provisioning remains a separate operation |
| Documentation and review | Drafted architecture, README, operations, validation and AI-use documentation; helped compare the repository with the PDF checklist | Reviewed terminology, corrected overbroad claims, recorded actual test evidence and separated verified results from unperformed infrastructure checks |

## 4. Prompts used

The original project conversation retained the following prompts verbatim. Spelling and wording are preserved as they appeared.

### 4.1 Repository bootstrap and requirement analysis — verbatim

> Can you go through the pdf file and csv file and write the steps as per given requirement to make project and help me to push into git. Need to completed all of the given feature for this project. like Make file README details step wise process, to setup the application as what are the prerequisites required before running application into any of the system. cloud as well local vm machine. Unit test 90% minimum e2e test.

This prompt established the repository scope: read the provided PDF and CSV, implement the requested features, provide a Makefile and step-by-step README, document prerequisites for local VM and cloud environments, and include a 90% unit-test coverage target plus E2E testing.

### 4.2 Code bootstrap — verbatim

> as per the plan can you write the code and create a git project to push with all requirements

This prompt requested implementation based on the previously discussed plan and creation of the Git project.

### 4.3 Unit-test prompt intent — summary, not a verbatim transcript

The exact wording of every unit-test follow-up was not retained as a separate prompt. The recorded request above explicitly included “Unit test 90% minimum e2e test.” Follow-up work asked for tests alongside the implementation and for a test/coverage approach covering the core application packages.

The resulting test work covered:
- CSV parsing and invalid input cases.
- Environment configuration and validation.
- API response, query validation and inclusive time-window behavior.
- Streamer and Collector behavior, including cancellation and failure paths.
- PostgreSQL queue behavior, including concurrent Streamer cursor updates, concurrent Collector claims, lease expiry, acknowledgement ownership, retry and dead-letter transitions.
- A black-box E2E test that observes telemetry through the running REST API.

### 4.4 Build-environment and documentation prompt intent — summary, not a verbatim transcript

The repository/bootstrap prompt above explicitly asked for a Makefile, detailed README, prerequisites and setup instructions for “cloud as well local vm machine.” Later requests expanded the installation and deployment guidance.

The resulting build/deployment documentation and scaffolding cover:
- Go dependencies, environment variables and PostgreSQL setup.
- Running the complete local environment with Docker Compose.
- Building the API, Streamer and Collector Docker images.
- Deploying application workloads with Helm.
- Cloud prerequisites such as a Kubernetes cluster, external PostgreSQL, registry access, secrets, network access and Metrics Server.
- CI checks for formatting, tests, coverage, OpenAPI consistency, Helm rendering and binary builds.

### 4.5 Other follow-up prompt themes — reconstructed summaries

The following are summaries of follow-up instructions from the development conversation, not claimed verbatim prompt quotations:

| Topic | Follow-up instruction in substance |
|---|---|
| README | Expand the README into an ordered installation procedure, list prerequisites, and explain local VM, Docker and cloud setup |
| API | Verify HTTP methods, endpoint paths, query parameters, timestamp filters and REST behavior against the assignment |
| Testing | Review test coverage, add E2E/system test execution and ensure the Makefile exposes a measurable 90% threshold |
| Deployment | Explain Docker image creation, Compose execution, Helm installation, external database configuration and cloud prerequisites |
| Audit | Compare the implemented repository against every PDF requirement and identify gaps that still need evidence |

These summaries are included to describe the work accurately. They are not a recovered transcript of every message.

## 5. What AI accelerated

AI assistance reduced the time needed to create and iterate on the initial project structure and supporting material, particularly:

- Converting a multi-page assignment into a practical implementation and delivery checklist.
- Exploring component boundaries and the custom database-backed queue design.
- Drafting Go code for parsers, services, API handlers, repositories and configuration.
- Creating a starting point for unit, integration and E2E test cases.
- Drafting Docker, Compose, Helm and CI configuration.
- Producing README, architecture, operations, validation and AI-use documentation.
- Reviewing CI feedback and suggesting focused corrective changes.

AI output was used as a starting point. It was not treated as proof that the system was correct, secure, scalable or production-ready.

## 6. Human decisions and manual interventions

The following points required engineering judgement, source review or test-based verification. They are recorded here because they materially affect correctness.

1. **GPU identity:** the CSV GPU UUID is used as the globally unique identifier. The GPU index can repeat on different hosts, so it is not sufficient as the global key.
2. **Timestamp semantics:** the original CSV timestamp is preserved as `source_timestamp`; the Streamer assigns `streamed_at`; the Collector assigns UTC `processed_at`. API ordering and inclusive `start_time` / `end_time` filters use `processed_at`, consistent with the assignment's processing-time requirement.
3. **Shared Streamer cursor:** multiple Streamers must coordinate through a shared database cursor. Queue insertion and cursor advancement are committed in the same transaction to avoid advancing the source without publishing the corresponding datapoint.
4. **Queue delivery semantics:** message delivery is at-least-once. A lease makes messages reclaimable after a consumer stops owning them; idempotent persistence keyed by message ID protects against duplicate delivery.
5. **Concurrent Collector claims:** PostgreSQL `FOR UPDATE SKIP LOCKED` is used so concurrent consumers claim disjoint queue rows. Integration tests were added to verify disjoint claims and lease recovery.
6. **Retry and dead-letter handling:** retry delay, maximum attempts, ownership checks and dead-letter transitions must be reviewed together; a message must not be acknowledged by a consumer whose lease has been replaced.
7. **Optional CSV fields:** `container`, `pod` and `namespace` may be empty in the supplied sample and are treated as optional.
8. **Pagination:** telemetry responses are bounded using `limit` and `offset`, and records have deterministic time ordering. Clients must follow pages to retrieve a large result set.
9. **Deployment boundary:** the Helm chart deploys application components; it expects an external PostgreSQL database and a pre-created Kubernetes Secret. It does not provision the cluster or managed database.
10. **Configuration behavior:** the Go programs read environment variables. A local `.env` file is a shell/Compose convenience and is not automatically loaded by the Go binaries.
11. **Coverage interpretation:** the 90% Makefile gate is calculated for five core packages. It is not a claim of 90% coverage across every package in the repository.
12. **Generated contract:** after CI regenerated the OpenAPI document and detected a difference, the committed `api/openapi.json` was refreshed from the Go contract and the CI consistency check was rerun.

## 7. Where AI output or prompts fell short

The initial broad prompts described the end goal but did not specify every implementation detail or validation assertion. The following gaps were found during review and corrected or explicitly documented:

| Gap found | Why the initial prompt/output was insufficient | Manual correction or treatment |
|---|---|---|
| Go formatting | Generated Go source was not consistently formatted | Ran `gofmt` across source and test files; CI format check subsequently passed |
| Missing module checksum file | The first repository version did not include `go.sum`, which also caused a Go dependency-cache warning in CI | Ran `go mod tidy` and committed `go.sum` and the required indirect dependency declarations |
| Generated OpenAPI mismatch | The checked-in JSON was not byte-for-byte current with the generator output | Regenerated `api/openapi.json` and verified the CI diff check |
| Queue scale/recovery evidence | A design description alone did not prove safe concurrent consumption or recovery after a consumer lease expired | Added and ran live PostgreSQL integration tests for concurrent claims and expired-lease reclamation |
| Coverage claim needed precision | A 90% threshold can be misunderstood as whole-repository coverage | Documented the exact five-package coverage scope and recorded the measured aggregate |
| Cloud/Kubernetes verification | Source and Helm templates cannot prove that a real cloud cluster, database, networking, secrets or HPA work | Marked live cluster deployment and cloud provisioning as not performed; retained the commands and prerequisites for that work |
| AI-use record was initially too brief | A feature list did not explain how AI was used, what prompts were used or which decisions needed human review | Expanded this file with the retained prompts, reconstructed prompt themes, workflow, manual interventions and limitations |

These corrections are part of the normal engineering review process. They should not be interpreted as a claim that AI independently discovered or fixed every issue.

## 8. Validation evidence for the AI-assisted implementation

The following CI run validates the merged PR candidate and records the checks performed:

- **GitHub Actions:** [CI Run #14](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/actions/runs/42621204383)
- **PR:** [PR #5 — Fix CI formatting and complete submission quality documentation](https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline/pull/5)
- **Code candidate tested by Run #14:** `c8d094ebad8c0d514c672b9862b9ccffba184c9b`
- **Merge commit:** `5c9efbad83239342b9bec2747dff7bb6c45f7990`

Run #14 completed successfully for the main validation and Docker Compose E2E jobs. Its checks included:
- Go formatting and `go vet`.
- Race-enabled Go tests, including PostgreSQL integration tests.
- Core-package coverage gate: **93.9%**, above the 90% minimum.
- OpenAPI generation and committed-file consistency.
- Helm lint and template rendering.
- API, Streamer and Collector binary builds.
- Docker Compose E2E using two Streamer and two Collector replicas.

The five-package coverage values recorded in that CI run were: API 91.4%, config 94.2%, telemetry 95.5%, Streamer 94.4%, and Collector 95.9%. PostgreSQL integration tests ran separately and are not part of that coverage aggregate.

The CI workflow's container-image publishing job is configured for pushes to `main`; it was intentionally skipped in a pull-request run. Compose E2E built and ran the local application stack, but this is not the same as publishing images to a registry.

## 9. Work not performed by AI and remaining limitations

- AI did not provision a cloud account, Kubernetes cluster, managed PostgreSQL service, DNS, ingress, certificates, external secrets manager or monitoring service.
- AI did not run a live Kubernetes HPA scale-up/down test in a target cluster. Helm lint and template rendering passed, but they are not a live cluster deployment.
- AI did not access production credentials or production GPU telemetry.
- The project uses the supplied CSV as sample telemetry input; a production DCGM agent or live GPU collection integration is outside the verified scope recorded here.
- The successful CI run provides evidence for the tested commit and configured environment only. It does not establish production readiness or a formal security assessment.

## 10. Final responsibility statement

AI assistance accelerated requirement analysis, code and test drafting, infrastructure scaffolding and documentation. The maintainer reviewed the design decisions, integrated the changes, responded to test/CI failures, and remains accountable for the correctness and operation of the final project. Any future change to code or deployment configuration must be revalidated; the CI results above apply to the identified candidate commit, not automatically to later changes.

# AI assistance disclosure

This repository was developed with AI assistance. This file records the areas where AI accelerated the work and where manual review/verification is required. The maintainer should update it with the actual final tool names, prompts, corrections and execution results before submission.

## AI-assisted areas and manual review

| Area | AI assistance | Manual review required |
|---|---|---|
| Requirement analysis | Converted supplied PDF into checklist; inspected CSV schema/cardinalities | Verify each item against assignment text and exact dataset |
| Architecture | Drafted Go components, PostgreSQL-backed custom queue, source cursor and delivery design | Review transaction isolation, locking, retry behavior and scaling assumptions |
| Go implementation | Assisted parser, Streamer, Collector, queue adapter, REST API and OpenAPI scaffolding | Run race tests, inspect resource cleanup and DB error paths |
| Unit tests | Drafted parser/config/API/Streamer/Collector tests | Review meaningful assertions and add regressions after changes |
| Packaging | Drafted Dockerfile, Compose, Helm and CI | Run image build, Compose E2E, Helm lint/template and cluster smoke test |
| Documentation | Drafted README, architecture, operations and setup material | Verify exact versions/commands against target environment and update manual work |

## Prompts establishing the scope

> Can you go through the pdf file and csv file and write the steps as per given requirement to make project and help me to push into git. Need to completed all of the given feature for this project. like Make file README details step wise process, to setup the application as what are the prerequisites required before running application into any of the system. cloud as well local vm machine. Unit test 90% minimum e2e test.

> as per the plan can you write the code and create a git project to push with all requirements

Workflow: inspect assignment and CSV; design component boundaries and queue; implement Go services and persistence; create tests and OpenAPI; add Makefile, Docker, Compose, Helm, CI and operations docs; run available local checks.

## Where generated output required manual intervention

1. The PDF asks for processing-time timestamp; the CSV also contains source `timestamp`. Keep both and filter on `processed_at`.
2. `gpu_id` repeats across hosts; use the GPU UUID for global identity.
3. Streamers need a shared transactional source cursor to avoid duplicate/repeated work.
4. Delivery must be described as at-least-once; idempotent writes handle redelivery.
5. `container`, `pod` and `namespace` are empty in the supplied sample; treat as optional.
6. The generation environment could not run Docker/Helm or reach module proxies. Execute database integration, Compose E2E, Docker builds, Helm validation and cluster smoke tests before claiming production readiness.

## Maintainer checklist

- [ ] Add exact AI tool names and prompts used in final development.
- [ ] Record manual edits and why the generated output needed correction.
- [ ] Record actual test/build output after review.
- [ ] Confirm the final GitHub commit matches the reviewed source.

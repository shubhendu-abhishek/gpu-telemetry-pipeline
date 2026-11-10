# Operations runbook

## Single-VM hosting

For a real Ubuntu VM deployment, follow [SINGLE_VM_DEPLOYMENT.md](SINGLE_VM_DEPLOYMENT.md). Compose binds the API and PostgreSQL only to host loopback; use an SSH tunnel to query the API from a workstation. Do not publish ports 5432 or 8080 directly.

## Local validation

```bash
make doctor
make fmt
make vet
make lint
make test
make coverage
make openapi
```

`make coverage` enforces 90% aggregate statement coverage across API, config, telemetry parser/source, Streamer and Collector. PostgreSQL behavior is tested separately with a live DB; E2E runs the complete Compose stack.

## Start and stop

```bash
make compose-up
curl -fsS http://localhost:8080/readyz
make compose-down
```

`make compose-down` preserves the database volume. Reset it with `docker compose -f deployments/docker-compose.yaml down -v`.

## Inspect queue state

```sql
SELECT status, COUNT(*) FROM queue_messages GROUP BY status ORDER BY status;
SELECT id, attempts, consumer_id, lease_until, last_error, created_at
FROM queue_messages WHERE status = 'dead' ORDER BY created_at DESC LIMIT 50;
SELECT source_id, next_offset, header_offset, updated_at FROM source_cursors;
```

Inspect dead-letter messages before replay; do not blindly reset retry counts.

## Backup

Use managed PostgreSQL backup/PITR in cloud. Local Compose backup:

```bash
docker compose -f deployments/docker-compose.yaml exec -T postgres \
  pg_dump -U gpu_telemetry -d gpu_telemetry > gpu_telemetry.sql
```

## Troubleshooting

- `/readyz` is 503: verify DSN, PostgreSQL DNS/network, credentials and migration logs.
- Streamer does not publish: verify CSV readability, required headers and stable `TELEMETRY_SOURCE_ID` across replicas.
- GPU list is empty: check Collectors, queue states and logs.
- Messages retry repeatedly: inspect `last_error`, DB constraints, payload and DB permissions.
- HPA does not scale: verify Metrics Server and CPU requests.
- Helm pods report Secret errors: create the configured DB Secret or enable chart secret creation with secured values.

## Scaling

Docker Compose:

```bash
docker compose -f deployments/docker-compose.yaml up -d --scale streamer=3 --scale collector=4
```

Kubernetes HPA supports 1–10 replicas configured in Helm values; verify Metrics Server. All replicas coordinate through PostgreSQL.

See [VALIDATION.md](VALIDATION.md) for executed checks and environment-dependent checks still outstanding.

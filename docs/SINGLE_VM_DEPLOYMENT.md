# Single Linux VM deployment and live acceptance test

This runbook deploys the GPU Telemetry Pipeline to one real Linux VM using Docker Compose. It is intended for a hosted functional test and a small controlled environment, not a highly available production deployment.

The deployment uses the existing PostgreSQL-backed custom queue and the existing API, Streamer and Collector containers. No Kubernetes cluster, physical GPU, CUDA toolkit or NVIDIA driver is needed for the supplied CSV workflow.

## 1. VM target and network design

Recommended starting VM:

| Item | Recommendation |
|---|---|
| OS | Ubuntu Server 24.04 LTS |
| CPU architecture | x86-64 / amd64 (the repository Dockerfile currently builds GOARCH=amd64) |
| Size | 4 vCPU and 8 GB RAM |
| Disk | 40 GB SSD to allow for images, logs and PostgreSQL data |
| Network | Static public IP or stable DNS name; inbound SSH only |
| Access | SSH key authentication and a non-root deployment account |

This is a practical test sizing recommendation, not a measured capacity guarantee. The VM is a single failure domain: the application and its PostgreSQL database stop if the VM is unavailable. Back up the database volume and do not treat this as HA production architecture.

### Security group / cloud firewall

Before starting the stack, set the VM's provider firewall/security group to allow:

- TCP 22 (SSH) only from your current trusted public IP/CIDR.
- No inbound TCP 5432 (PostgreSQL).
- No inbound TCP 8080 (API).
- No public HTTP/HTTPS listener unless you later add a reviewed reverse proxy, authentication/authorization and TLS.

The Compose file binds PostgreSQL and the API to 127.0.0.1 on the VM. Reach the API from your laptop over an SSH tunnel rather than opening port 8080.

## 2. Prepare Ubuntu and install Docker Engine

Connect with your SSH key:

~~~bash
ssh -i /path/to/your-key.pem <vm-user>@<VM_PUBLIC_IP>
~~~

Apply OS updates and install base utilities:

~~~bash
sudo apt-get update
sudo apt-get upgrade -y
sudo apt-get install -y ca-certificates curl git jq openssl ufw
~~~

Install Docker Engine and the Compose v2 plugin from Docker's official Ubuntu apt repository. These commands follow the [official Docker Ubuntu installation guide](https://docs.docker.com/engine/install/ubuntu/):

~~~bash
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF

sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io \
  docker-buildx-plugin docker-compose-plugin

sudo systemctl enable --now docker
sudo docker run --rm hello-world
sudo docker compose version
~~~

Docker administration is root-equivalent. For a small controlled VM, use sudo docker commands unless you intentionally accept that any account added to the docker group can effectively obtain root privileges.

Configure UFW after confirming SSH access is allowed. Keep the cloud firewall aligned with this policy:

~~~bash
sudo ufw allow OpenSSH
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw enable
sudo ufw status verbose
~~~

Do not add public UFW rules for ports 5432 or 8080.

## 3. Get the reviewed source

Clone the reviewed single-VM hardening branch. It contains the Compose configuration used by the rest of this runbook, including a VM-specific PostgreSQL password and loopback-only host port bindings:

~~~bash
git clone --branch deployment/single-vm-hardening --single-branch \
  https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline.git
cd gpu-telemetry-pipeline
git rev-parse --short HEAD
~~~

After this branch is merged into `main`, a fresh clone of the default branch can be used instead.

Confirm the host is the expected architecture and has enough disk/RAM:

~~~bash
uname -m
free -h
df -h /
~~~

The current multi-stage Dockerfile sets GOARCH=amd64; use an x86-64 VM unless the build configuration is deliberately changed and tested for another architecture.

## 4. Create a VM-only database password

Do not use the example development password on a reachable VM. The Compose configuration supports POSTGRES_PASSWORD interpolation and binds host ports to loopback.

Create a private local .env file. The password is a random 64-character hexadecimal value; it is not printed to the terminal:

~~~bash
{
  printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 32)"
  printf 'STREAMER_INTERVAL=25ms\n'
  printf 'COLLECTOR_WORKERS=2\n'
} > .env
chmod 600 .env
~~~

The 25ms Streamer interval is for an initial accelerated functional test of the 2,470-row fixture. It is not a throughput benchmark. After the acceptance test, you can change it to 500ms for the README's default pacing.

Verify Compose can resolve the configuration without printing its rendered secrets:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml config --quiet
~~~

The .env file is ignored by Git. Never commit it or paste its contents into chat or a ticket.

**Fresh-database note:** PostgreSQL applies POSTGRES_PASSWORD when it initializes an empty data directory. Changing .env later does not rotate the password inside an already initialized database volume. Use a planned SQL password rotation for an existing volume; do not delete the volume to change a password.

## 5. Build and start the application

Build the three application images on the VM and start PostgreSQL, API, Streamer and Collector:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml \
  up -d --build
~~~

Inspect service status and logs:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml ps
sudo docker compose --env-file .env -f deployments/docker-compose.yaml logs --tail=100 postgres api streamer collector
~~~

The API must report both liveness and database readiness:

~~~bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
~~~

Check the binding from the VM:

~~~bash
sudo ss -lntp | grep -E ':(5432|8080)'
~~~

The expected host bindings are 127.0.0.1:5432 and 127.0.0.1:8080, not 0.0.0.0 or the VM's public interface.

## 6. Access the API securely from your workstation

Open a second terminal on your workstation and create an SSH local port-forward:

~~~bash
ssh -i /path/to/your-key.pem -N \
  -L 8080:127.0.0.1:8080 <vm-user>@<VM_PUBLIC_IP>
~~~

While that SSH session remains connected, use the API locally from your workstation:

~~~bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/api/v1/gpus | jq
curl -fsS http://127.0.0.1:8080/openapi.json | jq '.openapi, .info'
~~~

Swagger UI is available at http://127.0.0.1:8080/swagger. Its HTML is served by the API; the interactive Swagger UI assets load from the public CDN, so your workstation browser needs outbound internet access.

The API has no built-in authentication middleware. Keep it private and do not replace the SSH tunnel with direct public exposure.

## 7. Live application acceptance tests

The sample contains 2,470 rows, ten metrics, 247 GPU UUIDs and 31 hosts. With STREAMER_INTERVAL=25ms, allow at least two minutes for the initial complete pass and queue drain on a small VM; actual processing time depends on the VM and database. The Streamer loops the CSV after EOF, so row counts can continue increasing after the first pass.

### 7.1 Verify GPU inventory

~~~bash
curl -fsS http://127.0.0.1:8080/api/v1/gpus | jq
~~~

The expected inventory after all host/GPU datapoints have been consumed is 247 GPU UUIDs. The API response's count should converge to 247.

### 7.2 Query telemetry for one real GPU

~~~bash
GPU_UUID="$(curl -fsS http://127.0.0.1:8080/api/v1/gpus | jq -r '.items[0].uuid')"
test -n "$GPU_UUID" && test "$GPU_UUID" != "null"
curl -fsS "http://127.0.0.1:8080/api/v1/gpus/$GPU_UUID/telemetry?limit=10&offset=0" | jq
~~~

Use the UUID returned by the API rather than assuming that a host-local GPU index is globally unique.

### 7.3 Verify PostgreSQL and data distribution

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml \
  exec -T postgres psql -U gpu_telemetry -d gpu_telemetry -c \
  "SELECT COUNT(*) AS telemetry_rows,
          COUNT(DISTINCT gpu_uuid) AS gpu_count,
          COUNT(DISTINCT metric_name) AS metric_count
   FROM telemetry_records;"
~~~

After a complete fixture pass, expect at least 247 distinct GPUs and ten distinct metrics. The total record count can exceed 2,470 because the Streamer loops the source repeatedly.

Check queue states and dead-letter messages:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml \
  exec -T postgres psql -U gpu_telemetry -d gpu_telemetry -c \
  "SELECT status, COUNT(*) FROM queue_messages GROUP BY status ORDER BY status;"

sudo docker compose --env-file .env -f deployments/docker-compose.yaml \
  exec -T postgres psql -U gpu_telemetry -d gpu_telemetry -c \
  "SELECT id, attempts, last_error, created_at
   FROM queue_messages WHERE status='dead'
   ORDER BY created_at DESC LIMIT 20;"
~~~

Review any dead-letter rows rather than resetting them blindly.

### 7.4 Test multi-replica coordination on the VM

After the first single-replica smoke test, scale Streamer and Collector to two containers each:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml \
  up -d --scale streamer=2 --scale collector=2
sudo docker compose --env-file .env -f deployments/docker-compose.yaml ps
~~~

The Streamers coordinate through the shared PostgreSQL source cursor. The Collectors claim queue rows using PostgreSQL row locks. This validates multi-container concurrency on one host; it does not test host-level failure or Kubernetes HPA.

### 7.5 Test process restart and persistence

Restart the Collector containers and confirm processing resumes:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml restart collector
sudo docker compose --env-file .env -f deployments/docker-compose.yaml ps
curl -fsS http://127.0.0.1:8080/readyz
~~~

Re-run the SQL counts in section 7.3. The PostgreSQL named volume should preserve data across container restarts. A controlled VM reboot can be tested separately during an approved maintenance window.

### 7.6 Observe host resource use

~~~bash
free -h
df -h
sudo docker stats --no-stream
sudo docker compose --env-file .env -f deployments/docker-compose.yaml logs --since=10m api streamer collector
~~~

Record CPU, memory, disk use, queue state, API response behavior and the number of GPUs/metrics observed. This is a functional smoke test, not a load or capacity benchmark.

## 8. Backup, restart and stop

Create a database dump before upgrades or experiments:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml \
  exec -T postgres pg_dump -U gpu_telemetry -d gpu_telemetry \
  > "$HOME/gpu_telemetry-$(date +%Y%m%d-%H%M%S).sql"
~~~

Copy the dump to secure storage outside the VM and periodically test restoration to a separate database. A backup that has not been restored is not a verified recovery plan.

Stop the containers but preserve the PostgreSQL volume:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml down
~~~

Start the existing stack again:

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml up -d
~~~

**Destructive reset — do not run on a machine with data you need:**

~~~bash
sudo docker compose --env-file .env -f deployments/docker-compose.yaml down -v
~~~

## 9. Rollout checklist and known limitations

Record these results when testing the VM:

| Check | Acceptance |
|---|---|
| Host | Ubuntu VM, x86-64, actual CPU/RAM/disk recorded |
| Network | Only SSH public inbound; API and PostgreSQL loopback-bound |
| Secrets | Random VM-specific DB password; .env mode 600; not committed |
| Containers | PostgreSQL, API, Streamer and Collector healthy/running |
| Liveness/readiness | /healthz and /readyz return HTTP 200 |
| GPU inventory | 247 unique GPU UUIDs after the fixture pass |
| Telemetry | API returns records for a UUID from the inventory |
| Metrics coverage | Ten distinct metric names after a complete pass |
| Queue | No unexpected dead-letter messages; retry state investigated |
| Multi-replica | Two Streamer and two Collector containers run together |
| Restart | Collector restart completes and API/data remain available |
| Resources | CPU, memory and disk usage recorded |
| Backup | SQL dump created and stored away from the VM |

Known constraints:
- This is one VM, so it is not highly available.
- No TLS termination, public DNS or reverse proxy is deployed by this guide.
- The API has no built-in authentication middleware. Keep it loopback/private.
- PostgreSQL is on the same VM and shares its disk/failure domain.
- The Collector currently does not renew leases during long-running claimed batches; keep processing within the configured lease or plan a lease-renewal hardening change.
- A Docker Compose replica count is not Kubernetes HPA. HPA scale testing remains a Kubernetes task.
- The application reads the supplied CSV; this guide does not add a live DCGM exporter or physical GPU collector.

For a public-facing or highly available environment, use the separate [production cloud runbook](PRODUCTION_DEPLOYMENT.md), and first add authentication/authorization plus a reviewed TLS gateway.

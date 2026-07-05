# 7. Deployment View & Production Deployment Guide

This chapter is the **complete, step-by-step deployment manual** for the Distributed VOD Transcoding Engine. It is written for developers who have never deployed a distributed system before, and for senior engineers who need a quick reference. Every port number, bucket name, environment variable, and CLI flag documented here is verified against the actual source code in this repository.

**Files Referenced Throughout This Guide**:

| File | What It Contains |
| :--- | :--- |
| [`Dockerfile`](../Dockerfile) | Two-stage Alpine build: compiles Go binary → copies into minimal runtime with FFmpeg |
| [`docker-compose.yml`](../docker-compose.yml) | Basic development stack (Redis, NATS, Etcd, MinIO) without compute nodes |
| [`docker-compose.prod.yml`](../docker-compose.prod.yml) | Full production-ready stack with profile-separated infrastructure and compute tiers |
| [`docker-compose-multiregion.yml`](../docker-compose-multiregion.yml) | Dual-region simulation (US-East + EU-West) with separate infra per region |
| [`configs/docker.yaml`](../configs/docker.yaml) | Default config for single-region Docker Compose deployment |
| [`configs/us-east.yaml`](../configs/us-east.yaml) | Config for US-East region in multi-region mode |
| [`configs/eu-west.yaml`](../configs/eu-west.yaml) | Config for EU-West region in multi-region mode |
| [`start.sh`](../start.sh) | One-command launcher for the single-region Docker stack |
| [`scripts/run_simulation.sh`](../scripts/run_simulation.sh) | One-command launcher for the multi-region simulation |
| [`scripts/simulate_crr.go`](../scripts/simulate_crr.go) | Cross-Region Replication simulator (copies manifests between MinIO buckets) |
| [`cmd/transcoder/main.go`](../cmd/transcoder/main.go) | Cobra CLI entrypoint — defines the `server gateway`, `server coordinator`, `server worker` sub-commands |
| [`internal/config/config.go`](../internal/config/config.go) | Unified `Config` struct + environment variable override logic |

---

## 7.1 How the Binary Is Built

The entire engine compiles into a **single static binary** called `video-engine`. This binary can run as a Gateway, a Coordinator, or a Worker — the role is chosen at startup via command-line arguments. There is no separate binary per role.

### 7.1.1 Building with Docker (Recommended)

The [`Dockerfile`](../Dockerfile) uses a two-stage build process. Here is the actual file from the repository, annotated line-by-line:

```dockerfile
# ─── Stage 1: Build the Go binary ───────────────────────────────────────────
# This stage uses a full Go toolchain image (~300 MB) to compile the code.
# After compilation, this entire stage is discarded — nothing from it appears
# in the final image except the compiled binary.
FROM golang:1.22-alpine AS builder

# Set the working directory inside the build container
WORKDIR /app

# Copy dependency manifests first. Docker caches this layer, so if go.mod
# and go.sum haven't changed, `go mod download` is skipped on subsequent builds.
COPY go.mod go.sum ./
RUN go mod download

# Now copy the full source tree. Any code change invalidates this cache layer.
COPY . .

# Compile a fully static binary. CGO_ENABLED=0 means no C library dependencies,
# so the binary runs on any Linux — even minimal Alpine with no glibc.
RUN CGO_ENABLED=0 GOOS=linux go build -o /video-engine ./cmd/transcoder

# ─── Stage 2: Create the minimal runtime image ─────────────────────────────
# This stage starts from a fresh Alpine base (~5 MB). Only the compiled binary
# and runtime dependencies are copied in. The final image is ≈80 MB.
FROM alpine:latest

# FFmpeg: Required by Worker nodes for video slicing and transcoding.
# ca-certificates: Required for HTTPS calls to AWS S3, Cloudflare R2, etc.
# bash: Useful for debugging inside the container (optional but helpful).
RUN apk add --no-cache ffmpeg ca-certificates bash

WORKDIR /app

# Copy ONLY the compiled binary from Stage 1
COPY --from=builder /video-engine /app/video-engine
RUN chmod +x /app/video-engine

# The binary accepts sub-commands: server gateway, server coordinator, server worker
ENTRYPOINT ["/app/video-engine"]
```

The [`.dockerignore`](../.dockerignore) file excludes `.git/`, `*.mp4` test videos, `transcoder-bin`, `logs/`, `docs/`, and `*.md` files from the Docker build context, keeping the image small and the build fast.

### 7.1.2 Building Without Docker (Native Compilation)

If you want to run the binary directly on a Linux server, macOS, or ARM device without Docker:

```bash
# Standard Linux (x86_64) — for Hetzner, DigitalOcean, AWS EC2, etc.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o video-engine ./cmd/transcoder

# ARM64 Linux — for Oracle Cloud Ampere A1, AWS Graviton, Raspberry Pi
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o video-engine ./cmd/transcoder

# macOS (Apple Silicon M1/M2/M3) — for local development
CGO_ENABLED=0 go build -o video-engine ./cmd/transcoder
```

The `-ldflags="-s -w"` flag strips debug symbols and DWARF tables, reducing the binary size by ~30%.

> **Important**: Workers require `ffmpeg` installed on the host system. On Ubuntu/Debian: `sudo apt install ffmpeg`. On Alpine: `apk add ffmpeg`. On macOS: `brew install ffmpeg`.

---

## 7.2 How the Binary Is Invoked (Cobra CLI)

The single binary runs as any of the three daemon roles via Cobra sub-commands defined in [`cmd/transcoder/main.go`](../cmd/transcoder/main.go):

```
video-engine server gateway      --config <path> --region <region>
video-engine server coordinator  --config <path> --region <region>
video-engine server worker       --config <path> --region <region>
```

**What each flag does**:
- `--config <path>` — Path to the YAML configuration file. Defaults to `config.yaml` in the current directory. Every config field can be overridden by environment variables (see §7.3).
- `--region <region>` — Region identifier string. Defaults to `us-east`. This value is embedded in job IDs and determines which S3 bucket and NATS subjects are used.

### Which Infrastructure Does Each Role Actually Initialize?

The [`initInfra`](../cmd/transcoder/main.go#L48) function selectively starts infrastructure drivers based on the role. Here is exactly what happens for each role:

| Role | Redis | NATS or SQS | Etcd | S3 (MinIO) | Why |
| :--- | :---: | :---: | :---: | :---: | :--- |
| **gateway** | ✅ | ✅ | ✅ | ✅ | Needs Redis for rate limiting + WebSocket progress streams. Needs NATS to bridge upload events. Needs Etcd for ring awareness. Needs S3 for presigned upload URLs. |
| **coordinator** | ✅ | ✅ | ✅ | ✅ | Needs Redis for job state bitmaps. Needs NATS to publish transcode tasks. Needs Etcd for partition ring membership + slicing locks. Needs S3 to read uploaded videos for GOP-aligned slicing. |
| **worker** | ✅ | ✅ | ✗ | ✅ | Needs Redis for idempotency checks + progress reporting. Needs NATS to pull tasks from shard queues. Needs S3 to download video segments and upload transcoded output. **Does NOT need Etcd** — workers are stateless consumers and never join the coordinator hash ring. |

This is why in [`docker-compose.prod.yml`](../docker-compose.prod.yml#L113), the `worker` service's `depends_on` list includes `redis`, `nats`, and `minio` but **NOT** `etcd`.

### Message Bus Selection (NATS vs. SQS)

The code in [`main.go#L76-L86`](../cmd/transcoder/main.go#L76) checks `config.MessageBusProvider`:

```go
if cfg.MessageBusProvider == "sqs" {
    messageBus, err = infra.NewSQSBus(cfg.ObjectStore)
} else {
    messageBus, err = infra.NewNATSBus(cfg.NATS)
}
```

- **Default**: `"nats"` — used for local dev, self-hosted, GCP, and OCI deployments.
- **Set to `"sqs"`**: for AWS deployments where you want managed SQS FIFO queues instead of self-hosted NATS.

---

## 7.3 Configuration: YAML Files & Environment Overrides

### 7.3.1 How Configuration Loading Works

The [`LoadConfig`](../internal/config/config.go#L113) function follows this exact sequence:

1. **Read the YAML file** specified by `--config` (e.g. `configs/docker.yaml`).
2. **Parse it** into the unified [`Config`](../internal/config/config.go#L13) struct.
3. **Apply environment variable overrides** — if an env var is set (non-empty), it overwrites the corresponding YAML field.
4. **Propagate `NodeID`** — copies the global `Config.NodeID` to `Config.Worker.NodeID`.

This means: **YAML is the base, env vars win**. You can ship the same YAML file everywhere and customize per-deployment using only env vars.

### 7.3.2 Complete Environment Variable Catalog

These are the **exact** environment variable names checked by [`config.go#L126-L161`](../internal/config/config.go#L126):

| Environment Variable | What It Overrides | Format | Example Value |
| :--- | :--- | :--- | :--- |
| `TRANSCODER_REDIS_ADDRS` | `redis.addrs` | Comma-separated list | `"redis-0:6379,redis-1:6379,redis-2:6379"` |
| `TRANSCODER_REDIS_PASSWORD` | `redis.password` | Plain string | `"my-secure-redis-password"` |
| `TRANSCODER_NATS_URLS` | `nats.urls` | Comma-separated list | `"nats://nats-0:4222,nats://nats-1:4222"` |
| `TRANSCODER_ETCD_ENDPOINTS` | `etcd.endpoints` | Comma-separated list | `"etcd-0:2379,etcd-1:2379,etcd-2:2379"` |
| `TRANSCODER_S3_ENDPOINT` | `object_store.endpoint` | Host:port or domain | `"minio:9000"` or `"s3.amazonaws.com"` |
| `TRANSCODER_S3_ACCESS_KEY` | `object_store.access_key` | AWS-style access key | `"AKIAIOSFODNN7EXAMPLE"` |
| `TRANSCODER_S3_SECRET_KEY` | `object_store.secret_key` | AWS-style secret key | `"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"` |
| `TRANSCODER_S3_BUCKET` | `object_store.bucket` | Bucket name string | `"transcoder-docker"` |
| `TRANSCODER_JWT_SECRET` | `gateway.jwt_secret` | Secret string (32+ bytes) | `"my-super-secret-jwt-signing-key-here"` |
| `TRANSCODER_REGION` | `region` | Region identifier | `"us-east-1"` or `"docker-local"` |
| `TRANSCODER_LISTEN_ADDR` | `gateway.listen_addr` | IP:port | `"0.0.0.0:8080"` |
| `TRANSCODER_MESSAGE_BUS_PROVIDER` | `message_bus_provider` | `"nats"` or `"sqs"` | `"nats"` |

### 7.3.3 The Three Shipped Config Files

The repository ships three ready-to-use configuration presets in the [`configs/`](../configs/) directory:

#### [`configs/docker.yaml`](../configs/docker.yaml) — Single-Region Docker Compose

This is used by [`docker-compose.prod.yml`](../docker-compose.prod.yml). All services use Docker DNS names (`redis`, `nats`, `etcd`, `minio`) instead of IP addresses:

| Field | Value | Why |
| :--- | :--- | :--- |
| `region` | `"docker-local"` | Identifies this as a local Docker environment |
| `redis.addrs` | `["redis:6379"]` | Docker service name resolves inside the compose network |
| `object_store.endpoint` | `"minio:9000"` | MinIO S3 API on Docker's internal port |
| `object_store.bucket` | `"transcoder-docker"` | Created by the `minio-init` container on first boot |
| `gateway.listen_addr` | `"0.0.0.0:8080"` | Binds to all interfaces so Docker can forward port 8080 |
| `coordinator.partition_count` | `4` | Low for dev (production uses 1024) |
| `coordinator.nats_shard_count` | `2` | Must evenly divide `partition_count` (4 ÷ 2 = 2 ✓) |
| `worker.scratch_dir` | `"/tmp/scratch-docker"` | Temp directory for FFmpeg intermediate files |
| `worker.hw_accel` | `"none"` | No GPU acceleration in Docker (use `"nvenc"` for NVIDIA GPUs) |
| `metrics.listen_addr` | `"0.0.0.0:9091"` | Prometheus metrics endpoint |

#### [`configs/us-east.yaml`](../configs/us-east.yaml) — Multi-Region US-East

| Field | Value | Difference from docker.yaml |
| :--- | :--- | :--- |
| `region` | `"us-east"` | Regional identifier |
| `redis.addrs` | `["127.0.0.1:6379"]` | Localhost (runs natively, not in Docker network) |
| `nats.urls` | `["nats://127.0.0.1:4222"]` | Localhost |
| `etcd.endpoints` | `["127.0.0.1:2379"]` | Localhost |
| `object_store.endpoint` | `"127.0.0.1:9000"` | MinIO on standard port |
| `object_store.bucket` | `"transcoder-us-east"` | Region-specific bucket |
| `gateway.listen_addr` | `"127.0.0.1:8080"` | Standard gateway port |
| `worker.scratch_dir` | `"/tmp/scratch-us-east"` | Region-specific scratch dir |
| `metrics.listen_addr` | `"127.0.0.1:9091"` | Standard metrics port |

#### [`configs/eu-west.yaml`](../configs/eu-west.yaml) — Multi-Region EU-West

| Field | Value | Why It Differs |
| :--- | :--- | :--- |
| `region` | `"eu-west"` | Different region identifier |
| `redis.addrs` | `["127.0.0.1:6389"]` | **Port offset** — avoids collision with US-East's `:6379` |
| `nats.urls` | `["nats://127.0.0.1:4232"]` | **Port offset** from `:4222` |
| `etcd.endpoints` | `["127.0.0.1:2389"]` | **Port offset** from `:2379` |
| `object_store.endpoint` | `"127.0.0.1:9010"` | **Port offset** from `:9000` |
| `object_store.bucket` | `"transcoder-eu-west"` | Different bucket per region |
| `gateway.listen_addr` | `"127.0.0.1:8090"` | **Port offset** from `:8080` |
| `worker.scratch_dir` | `"/tmp/scratch-eu-west"` | Separate scratch per region |
| `metrics.listen_addr` | `"127.0.0.1:9092"` | **Port offset** from `:9091` |

---

## 7.4 Local Developer Setup (Docker Compose)

### 7.4.1 Prerequisites

Before you begin, make sure you have these tools installed:

| Tool | Minimum Version | How to Check | How to Install |
| :--- | :--- | :--- | :--- |
| **Docker** | 24.0+ | `docker --version` | [docs.docker.com/get-docker](https://docs.docker.com/get-docker/) |
| **Docker Compose** | v2.20+ (built into Docker Desktop) | `docker compose version` | Included with Docker Desktop |
| **Go** | 1.22+ (only for native/multi-region builds) | `go version` | [go.dev/dl](https://go.dev/dl/) |
| **FFmpeg** | 5.0+ (only for native builds) | `ffmpeg -version` | `brew install ffmpeg` / `apt install ffmpeg` |
| **Git** | Any modern version | `git --version` | Pre-installed on most systems |

### 7.4.2 Single-Region Local Stack (One-Command Start)

**Uses**: [`docker-compose.prod.yml`](../docker-compose.prod.yml) + [`configs/docker.yaml`](../configs/docker.yaml) + [`start.sh`](../start.sh)

This is the fastest way to get the entire engine running on your laptop:

```bash
# 1. Clone the repository
git clone https://github.com/your-org/distributed-transcoder.git
cd distributed-transcoder

# 2. Make the start script executable and run it
chmod +x start.sh
./start.sh
```

**What [`start.sh`](../start.sh) does internally** (3 steps):

```
Step [1/3] — docker compose -f docker-compose.prod.yml build
             Compiles the Go binary inside the Dockerfile (cached after first run)

Step [2/3] — docker compose -f docker-compose.prod.yml --profile infra-selfhosted --profile backend up -d
             Boots all infrastructure + compute containers in the background

Step [3/3] — docker compose -f docker-compose.prod.yml ps
             Waits 5 seconds, then prints the health status of all containers
```

After `start.sh` completes, you'll see this output:

```
============================================================
 Successfully started!
============================================================
 Useful Endpoints:
 - Gateway API & WebSockets : http://localhost:8080
 - MinIO S3 Console         : http://localhost:9001 (minioadmin / minioadmin)

 To scale the transcoder workers on the fly, run:
 docker compose -f docker-compose.prod.yml up -d --scale worker=5

 To view logs:
 docker compose -f docker-compose.prod.yml logs -f
============================================================
```

#### What Containers Are Running?

**Infrastructure Tier** (started by `--profile infra-selfhosted`):

| Container | Image | External Port(s) | Internal Purpose |
| :--- | :--- | :--- | :--- |
| `redis` | `redis:7.0-alpine` | `6379` | Job state bitmaps, WebSocket progress streams, rate limiting counters |
| `nats` | `nats:2.9-alpine` | `4222` (client), `8222` (monitoring dashboard) | JetStream task queues, dead letter queues |
| `etcd` | `gcr.io/etcd-development/etcd:v3.5.9` | `2379` | Coordinator hash ring membership, slicing mutual-exclusion locks |
| `minio` | `minio/minio:RELEASE.2023-08-09T23-30-22Z` | `9000` (S3 API), `9001` (web console) | S3-compatible object storage for video files |
| `minio-init` | `minio/mc` | — | One-shot init container: creates `transcoder-docker` bucket, sets public anonymous access policy |

**Compute Tier** (started by `--profile backend`):

| Container | Start Command | External Port | Replicas |
| :--- | :--- | :--- | :--- |
| `gateway` | `server gateway --config /app/configs/docker.yaml` | `8080` | 1 |
| `coordinator` | `server coordinator --config /app/configs/docker.yaml` | — (no external port) | 1 |
| `worker` | `server worker --config /app/configs/docker.yaml` | — (no external port) | **2** (set by `deploy.replicas: 2` in compose file) |

#### Common Developer Operations

```bash
# Scale workers up (e.g. to process jobs faster during testing)
docker compose -f docker-compose.prod.yml up -d --scale worker=8

# View real-time logs from all services
docker compose -f docker-compose.prod.yml logs -f

# View logs from only the workers
docker compose -f docker-compose.prod.yml logs -f worker

# Stop everything and destroy volumes (full reset)
docker compose -f docker-compose.prod.yml --profile infra-selfhosted --profile backend down -v

# Restart just the gateway after code changes
docker compose -f docker-compose.prod.yml build gateway
docker compose -f docker-compose.prod.yml up -d gateway
```

---

### 7.4.3 Multi-Region Local Simulation

**Uses**: [`docker-compose-multiregion.yml`](../docker-compose-multiregion.yml) + [`configs/us-east.yaml`](../configs/us-east.yaml) + [`configs/eu-west.yaml`](../configs/eu-west.yaml) + [`scripts/run_simulation.sh`](../scripts/run_simulation.sh) + [`scripts/simulate_crr.go`](../scripts/simulate_crr.go)

This mode boots **two fully isolated regional stacks** on a single developer machine, proving that jobs submitted to US-East are processed only by US-East workers, and vice versa.

```bash
chmod +x scripts/run_simulation.sh
scripts/run_simulation.sh
```

**What [`run_simulation.sh`](../scripts/run_simulation.sh) does internally** (6 steps):

1. **Cleanup handler** — Registers `trap cleanup EXIT SIGINT SIGTERM` so pressing Ctrl+C kills all processes and runs `docker compose down -v`.
2. **Creates scratch directories** — `mkdir -p /tmp/scratch-us-east /tmp/scratch-eu-west`.
3. **Boots Docker infrastructure** — `docker compose -f docker-compose-multiregion.yml up -d` starts two independent Redis, NATS, Etcd, and MinIO instances.
4. **Compiles Go binary natively** — `go build -o transcoder-bin ./cmd/transcoder/main.go`.
5. **Launches 6 daemon processes** as background tasks:
   - **US-East** (3 processes): `transcoder-bin server gateway --config configs/us-east.yaml --region us-east`, plus coordinator and worker using the same config.
   - **EU-West** (3 processes): `transcoder-bin server gateway --config configs/eu-west.yaml --region eu-west`, plus coordinator and worker.
6. **Launches the CRR Simulator** — `go run scripts/simulate_crr.go` starts a bidirectional manifest replication loop between the two MinIO buckets.

#### Multi-Region Port & Bucket Matrix

Both regions run simultaneously on your machine using **port offsets** to avoid collisions:

| Resource | US-East | EU-West | Port Offset |
| :--- | :--- | :--- | :--- |
| **Gateway API** | `:8080` | `:8090` | +10 |
| **Redis** | `:6379` | `:6389` | +10 |
| **NATS Client** | `:4222` | `:4232` | +10 |
| **NATS Monitor** | `:8222` | `:8232` | +10 |
| **MinIO S3 API** | `:9000` | `:9010` | +10 |
| **MinIO Console** | `:9001` | `:9011` | +10 |
| **Metrics** | `:9091` | `:9092` | +1 |
| **S3 Bucket** | `transcoder-us-east` | `transcoder-eu-west` | — |
| **Scratch Dir** | `/tmp/scratch-us-east` | `/tmp/scratch-eu-west` | — |

#### Testing Regional Isolation

```bash
# Submit a job to US-East Gateway
curl -X POST http://127.0.0.1:8080/api/jobs/upload-session \
  -H 'Content-Type: application/json' \
  -d '{"file_size_bytes": 1048576, "file_name": "test.mp4", "content_type": "video/mp4"}'

# Watch US-East worker process the job
tail -f logs/us-east-worker.log

# Watch EU-West worker — it should remain completely idle (proving job isolation)
tail -f logs/eu-west-worker.log

# Watch CRR — after job completes, manifests replicate from US-East → EU-West
tail -f logs/simulate-crr.log
```

#### How the CRR Simulator Works

The [`simulate_crr.go`](../scripts/simulate_crr.go) program runs a polling loop every **2 seconds** that:
1. Lists all objects in both MinIO buckets (`transcoder-us-east` on `:9000` and `transcoder-eu-west` on `:9010`).
2. Filters to **manifest files only** — `master.m3u8`, `manifest.mpd`, `job_completed.json`, `job_manifest.json`. Raw video segments are NOT replicated (data gravity principle).
3. Compares ETag and size between source and destination. If the manifest is missing or different, it copies it.
4. Replication is **bidirectional** — US-East → EU-West AND EU-West → US-East.

---

## 7.5 Self-Hosted Deployment (Bare-Metal / VPS)

### 7.5.1 Small-Scale: Single Server (up to 100 Concurrent Jobs)

Run the full stack on a single dedicated server using Docker Compose. This is identical to the local dev setup but on a remote machine.

**Recommended Hardware**: 8+ CPU cores, 32 GB RAM, 500 GB SSD. Examples: Hetzner AX41 (€44/mo), DigitalOcean Droplet (8 vCPU), Contabo VDS.

```bash
# 1. SSH into your server
ssh root@your-server-ip

# 2. Install Docker (if not already installed)
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER  # Add your user to the docker group
# Log out and back in for group membership to take effect

# 3. Clone the repository
git clone https://github.com/your-org/distributed-transcoder.git /opt/video-engine
cd /opt/video-engine

# 4. Boot the full stack
docker compose -f docker-compose.prod.yml --profile infra-selfhosted --profile backend up -d

# 5. Scale workers based on your CPU cores (rule of thumb: cores ÷ 2)
docker compose -f docker-compose.prod.yml up -d --scale worker=4

# 6. Verify all containers are healthy
docker compose -f docker-compose.prod.yml ps
```

**Adding HTTPS with Caddy** (recommended for production):

```bash
# Install Caddy
sudo apt install -y caddy

# Create a Caddyfile for automatic HTTPS
sudo tee /etc/caddy/Caddyfile << 'EOF'
vod-api.yourdomain.com {
    reverse_proxy localhost:8080
}
minio-console.yourdomain.com {
    reverse_proxy localhost:9001
}
EOF

# Reload Caddy — it automatically obtains Let's Encrypt certificates
sudo systemctl reload caddy
```

**Persistence**: Docker named volumes (`redis_data`, `nats_data`, `etcd_data`, `minio_data`) are defined in [`docker-compose.prod.yml`](../docker-compose.prod.yml#L118) and persist across container restarts. To backup MinIO data: `docker run --rm -v minio_data:/data alpine tar czf /backup.tar.gz /data`.

---

### 7.5.2 Large-Scale: Kubernetes Cluster (K3s / RKE2 / kubeadm)

For production workloads requiring hundreds or thousands of workers, deploy on a Kubernetes cluster. The fleet sizing below comes from [`infrastructure_requirements.md`](../infrastructure_requirements.md):

| Tier | Fleet Size | Hardware Per Node | K8s Workload Type | Scaling Metric |
| :--- | :--- | :--- | :--- | :--- |
| **Gateway** | ~100 nodes at 50M users | `4 vCPU / 16 GB RAM / 10 Gbps NIC` | `Deployment` + `Ingress` | HPA on CPU (70% threshold) |
| **Coordinator** | ~50 nodes | `8 vCPU / 64 GB RAM` (high RAM for 1024 partition maps) | `StatefulSet` with `podAntiAffinity` | Fixed count (ring-based) |
| **Worker** | ~10,000 nodes | `4 vCPU / 16 GB RAM / 1× NVIDIA T4 GPU / 500 GB NVMe SSD` | `Deployment` + KEDA | Queue depth (NATS consumer lag) |

**Infrastructure services** (deploy via Helm charts):

| Service | Helm Chart | Node Count | Critical Requirement |
| :--- | :--- | :--- | :--- |
| **Redis Cluster** | Bitnami Redis Cluster | 6 (3 masters, 3 replicas) | 32 GB RAM per node. Hash-tag routing for `{job_uuid}` keys. |
| **NATS JetStream** | Official NATS Helm | 5 nodes | High-IOPS PVs for the Write-Ahead Log (WAL). |
| **Etcd** | Bitnami Etcd | 5 nodes | **NVMe-backed PVCs only.** Requires `<10ms` fsync latency. Network-attached block storage (EBS/PD) causes Raft leader election flapping. |
| **MinIO** | MinIO Operator | Distributed cluster | Dual-port 100 Gbps NICs for 1 TB/s peak bandwidth. |

**Network topology** (from [`infrastructure_requirements.md`](../infrastructure_requirements.md)):
- **Control Plane Network** (10/25 Gbps VLAN): NATS messages, Redis pipelines, Etcd heartbeats, WebSocket streams.
- **Storage Data Plane** (100 Gbps RDMA/RoCE): MinIO `GetObject`/`PutObject` calls. Video segment traffic must NEVER cross the control plane network.
- **External CDN**: Cloudflare / Fastly / Akamai fronts the MinIO Ingress for global video delivery.

---

### 7.5.3 Multi-Datacenter Mesh (Tailscale / WireGuard)

For running across multiple physical datacenters (e.g. Frankfurt + Singapore + Dallas) without a cloud provider:

```bash
# On EVERY bare-metal node in EVERY datacenter:
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up --authkey=tskey-auth-xxxxxx --advertise-tags=tag:vod-engine

# Example Tailscale IP allocation:
# ┌─────────────────────────────────────────────┐
# │ Frankfurt Datacenter                         │
# │   100.64.0.1 — Gateway + Redis               │
# │   100.64.0.2 — Coordinator + Etcd            │
# │   100.64.0.3 — Worker + NATS                 │
# │   100.64.0.4 — MinIO Storage                 │
# ├─────────────────────────────────────────────┤
# │ Singapore Datacenter                         │
# │   100.64.1.1 — Gateway + Redis               │
# │   100.64.1.2 — Coordinator + Etcd            │
# │   100.64.1.3 — Worker + NATS                 │
# │   100.64.1.4 — MinIO Storage                 │
# └─────────────────────────────────────────────┘
```

Each datacenter runs its **own** Redis, NATS, Etcd, and MinIO instances. Jobs stay local to the datacenter where they were uploaded. Cross-region manifest replication uses the CRR pattern from [`scripts/simulate_crr.go`](../scripts/simulate_crr.go).

---

## 7.6 Railway PaaS Deployment

Railway can host the engine as container services. Railway provides managed Redis as a plugin but does NOT offer managed S3, NATS, or Etcd, so those must be provisioned externally.

### 7.6.1 `railway.json` Configuration

Create this file in the repository root:

```json
{
  "$schema": "https://railway.app/railway.schema.json",
  "build": {
    "builder": "DOCKERFILE",
    "dockerfilePath": "Dockerfile"
  },
  "deploy": {
    "numReplicas": 1,
    "sleepApplication": false,
    "restartPolicyType": "ON_FAILURE",
    "restartPolicyMaxRetries": 10
  }
}
```

### 7.6.2 Railway Service Architecture

You need **three separate Railway services** pointing to the same repository, each with a different start command:

| Railway Service Name | Start Command Override | External Dependencies |
| :--- | :--- | :--- |
| `vod-gateway` | `/app/video-engine server gateway --config /app/configs/docker.yaml` | Redis (Railway Plugin), S3 (AWS/R2), NATS (external), Etcd (external) |
| `vod-coordinator` | `/app/video-engine server coordinator --config /app/configs/docker.yaml` | Same as Gateway |
| `vod-worker` (scale replicas ×N) | `/app/video-engine server worker --config /app/configs/docker.yaml` | Redis, S3, NATS only (**no Etcd**) |

### 7.6.3 Railway Environment Variables

Railway assigns a **dynamic `$PORT`** to each service. The Gateway MUST bind to it:

**Gateway Service**:
```ini
TRANSCODER_LISTEN_ADDR=0.0.0.0:${PORT}
TRANSCODER_REDIS_ADDRS=${REDIS_URL}
TRANSCODER_REDIS_PASSWORD=${REDIS_PASSWORD}
TRANSCODER_NATS_URLS=nats://your-external-nats:4222
TRANSCODER_ETCD_ENDPOINTS=your-external-etcd:2379
TRANSCODER_S3_ENDPOINT=s3.us-east-1.amazonaws.com
TRANSCODER_S3_BUCKET=your-railway-media-bucket
TRANSCODER_S3_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
TRANSCODER_S3_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
TRANSCODER_JWT_SECRET=your-jwt-signing-secret
TRANSCODER_REGION=railway-us
TRANSCODER_MESSAGE_BUS_PROVIDER=nats
```

**Worker Service** (subset — no `LISTEN_ADDR`, no `ETCD_ENDPOINTS`):
```ini
TRANSCODER_REDIS_ADDRS=${REDIS_URL}
TRANSCODER_REDIS_PASSWORD=${REDIS_PASSWORD}
TRANSCODER_NATS_URLS=nats://your-external-nats:4222
TRANSCODER_S3_ENDPOINT=s3.us-east-1.amazonaws.com
TRANSCODER_S3_BUCKET=your-railway-media-bucket
TRANSCODER_S3_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
TRANSCODER_S3_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
TRANSCODER_REGION=railway-us
TRANSCODER_MESSAGE_BUS_PROVIDER=nats
```

---

## 7.7 Amazon Web Services (AWS) Deployment

### 7.7.1 AWS Infrastructure Stack

| Component | AWS Service | Configuration Details |
| :--- | :--- | :--- |
| **Compute** | EKS (Elastic Kubernetes Service) | Managed node groups. Use `c6g.xlarge` (ARM Graviton) for Gateway/Coordinator, `g5.xlarge` (NVIDIA A10G GPU) for Workers. |
| **Object Storage** | S3 | Direct presigned PUT uploads from clients. Enable CORS on the bucket. |
| **State Cache** | ElastiCache for Redis (Cluster Mode) | Enable Cluster Mode for automatic hash-tag routing of `{job_uuid}` keys. |
| **Event Bus** | SQS FIFO | Set `TRANSCODER_MESSAGE_BUS_PROVIDER=sqs`. Create per-shard FIFO queues with `MessageGroupId`. |
| **Consensus** | Self-managed Etcd on EKS | Deploy as a `StatefulSet` with NVMe-backed PVCs (`io2` or `gp3` with provisioned IOPS) for `<10ms` fsync. |
| **Autoscaling** | KEDA with `aws-sqs-queue` trigger | Scale workers from 0 → 100 based on `ApproximateNumberOfMessages`. |
| **Identity** | IRSA (IAM Roles for Service Accounts) | No static AWS credentials stored in pods. |

### 7.7.2 Step-by-Step AWS Setup

**1. Create the S3 Bucket with CORS**:
```bash
aws s3api create-bucket \
  --bucket vod-media-prod \
  --region us-east-1

aws s3api put-bucket-cors \
  --bucket vod-media-prod \
  --cors-configuration '{
    "CORSRules": [{
      "AllowedHeaders": ["*"],
      "AllowedMethods": ["PUT","POST","GET","HEAD"],
      "AllowedOrigins": ["*"],
      "ExposeHeaders": ["ETag"],
      "MaxAgeSeconds": 3000
    }]
  }'
```

**2. Create SQS FIFO Queues** (one per NATS shard):
```bash
# Task queue (shard 0)
aws sqs create-queue \
  --queue-name transcode-tasks-shard-0.fifo \
  --attributes FifoQueue=true,ContentBasedDeduplication=true

# Dead letter queue
aws sqs create-queue \
  --queue-name transcode-tasks-dlq.fifo \
  --attributes FifoQueue=true
```

**3. Create the IRSA IAM Policy**:
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "S3Access",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:HeadObject",
        "s3:DeleteObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::vod-media-prod",
        "arn:aws:s3:::vod-media-prod/*"
      ]
    },
    {
      "Sid": "SQSAccess",
      "Effect": "Allow",
      "Action": [
        "sqs:SendMessage",
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:ChangeMessageVisibility",
        "sqs:GetQueueAttributes"
      ],
      "Resource": "arn:aws:sqs:us-east-1:123456789012:transcode-tasks-*"
    }
  ]
}
```

**4. Set Engine Environment Variables on EKS**:
```yaml
# Kubernetes ConfigMap for the VOD Engine
apiVersion: v1
kind: ConfigMap
metadata:
  name: vod-engine-config
data:
  TRANSCODER_S3_ENDPOINT: "s3.us-east-1.amazonaws.com"
  TRANSCODER_S3_BUCKET: "vod-media-prod"
  TRANSCODER_REGION: "us-east-1"
  TRANSCODER_MESSAGE_BUS_PROVIDER: "sqs"
  TRANSCODER_LISTEN_ADDR: "0.0.0.0:8080"
```

### 7.7.3 AWS Multi-Region Setup

For global AWS deployments across `us-east-1`, `eu-west-1`, and `ap-southeast-1`:

1. **Deploy independent EKS clusters** in each region, each with its own ElastiCache, SQS queues, and Etcd.
2. **S3 Cross-Region Replication (CRR)**: Enable S3 CRR rules to automatically replicate completed media from the primary bucket to secondary region buckets.
3. **Route 53 Latency-Based Routing**: Create a Route 53 hosted zone with latency-based records pointing to each region's Gateway ALB. Users are automatically routed to the nearest region.

---

## 7.8 Google Cloud Platform (GCP) Deployment

### 7.8.1 GCP Infrastructure Stack

| Component | GCP Service | Configuration Details |
| :--- | :--- | :--- |
| **Compute** | GKE (Autopilot or Standard) | Managed Kubernetes with pod autoscaling. Use `t2a-standard-4` (ARM Tau) for Gateway. |
| **Object Storage** | GCS via S3 Interoperability API | GCS provides an S3-compatible API via HMAC keys. The engine connects to it exactly like AWS S3. |
| **State Cache** | Cloud Memorystore for Redis | Private VPC peering. Basic tier for dev, Standard tier for production. |
| **Event Bus** | Self-managed NATS JetStream on GKE | Set `TRANSCODER_MESSAGE_BUS_PROVIDER=nats`. Deploy via official NATS Helm chart. |
| **Consensus** | Self-managed Etcd on GKE | Use local SSD PersistentVolumes for `<10ms` fsync. |
| **Identity** | GKE Workload Identity | Maps K8s ServiceAccounts to GCP IAM Service Accounts — no static credentials. |

### 7.8.2 Step-by-Step GCP Setup

**1. Create the GCS Bucket**:
```bash
gcloud storage buckets create gs://vod-media-gcp \
  --location=us-central1 \
  --uniform-bucket-level-access
```

**2. Generate S3-Compatible HMAC Keys**:

Go to GCP Console → Cloud Storage → Settings → **Interoperability** tab → Create a key for a service account. This gives you an Access Key and Secret Key that work with the AWS S3 SDK used by the engine.

**3. Set Up Workload Identity**:
```bash
# Create GCP IAM service account
gcloud iam service-accounts create vod-gke-sa \
  --project=my-gcp-project \
  --display-name="VOD Engine GKE Service Account"

# Grant storage access
gcloud storage buckets add-iam-policy-binding gs://vod-media-gcp \
  --member="serviceAccount:vod-gke-sa@my-gcp-project.iam.gserviceaccount.com" \
  --role="roles/storage.objectAdmin"

# Bind K8s ServiceAccount to GCP IAM SA (Workload Identity)
gcloud iam service-accounts add-iam-policy-binding \
  vod-gke-sa@my-gcp-project.iam.gserviceaccount.com \
  --role="roles/iam.workloadIdentityUser" \
  --member="serviceAccount:my-gcp-project.svc.id.goog[vod-engine/vod-engine-sa]"
```

**4. Set Engine Environment Variables**:
```ini
TRANSCODER_S3_ENDPOINT=storage.googleapis.com
TRANSCODER_S3_ACCESS_KEY=GOOG1EXXXXXXXXXX      # GCS HMAC access key
TRANSCODER_S3_SECRET_KEY=xxxxxxxxxxxxxxxxxxxxxxxx  # GCS HMAC secret key
TRANSCODER_S3_BUCKET=vod-media-gcp
TRANSCODER_REGION=us-central1
TRANSCODER_MESSAGE_BUS_PROVIDER=nats
```

---

## 7.9 Oracle Cloud Infrastructure (OCI) Deployment

### 7.9.1 OCI Free-Tier ARM Mesh (Zero-Cost Production)

Oracle Cloud's Always Free tier provides **4× Ampere A1 instances** (4 OCPUs, 24 GB RAM each). This is enough to run the full engine stack at no cost:

| OCI Instance | Role | Tailscale IP |
| :--- | :--- | :--- |
| Instance 1 | Gateway + Redis | `100.64.0.1` |
| Instance 2 | Coordinator + Etcd | `100.64.0.2` |
| Instance 3 | Worker + NATS | `100.64.0.3` |
| Instance 4 | Worker + MinIO | `100.64.0.4` |

**Step-by-Step Setup** (repeat on each instance):

```bash
# 1. Cross-compile the binary for ARM64 (run on your dev machine)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o video-engine ./cmd/transcoder

# 2. Copy binary and config to the remote instance
scp video-engine ubuntu@<instance-ip>:/usr/local/bin/
scp configs/us-east.yaml ubuntu@<instance-ip>:/etc/video-engine/config.yaml

# 3. SSH into the instance
ssh ubuntu@<instance-ip>

# 4. Install FFmpeg (required for Worker nodes)
sudo apt update && sudo apt install -y ffmpeg

# 5. Install Tailscale for encrypted inter-node communication
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up --authkey=tskey-auth-xxxxxx

# 6. Create a systemd service (example: Worker)
sudo tee /etc/systemd/system/video-engine-worker.service << 'EOF'
[Unit]
Description=VOD Engine Worker
After=network.target tailscaled.service

[Service]
Type=simple
User=ubuntu
ExecStart=/usr/local/bin/video-engine server worker --config /etc/video-engine/config.yaml --region oci-arm
Restart=always
RestartSec=5s
LimitNOFILE=65536
Environment=TRANSCODER_REDIS_ADDRS=100.64.0.1:6379
Environment=TRANSCODER_NATS_URLS=nats://100.64.0.3:4222
Environment=TRANSCODER_S3_ENDPOINT=100.64.0.4:9000
Environment=TRANSCODER_S3_BUCKET=transcoder-oci

[Install]
WantedBy=multi-user.target
EOF

# 7. Enable and start the service
sudo systemctl daemon-reload
sudo systemctl enable --now video-engine-worker

# 8. Verify it's running
sudo systemctl status video-engine-worker
sudo journalctl -u video-engine-worker -f  # Watch live logs
```

### 7.9.2 OCI Managed Kubernetes (OKE)

For larger Oracle deployments, use Oracle Container Engine for Kubernetes (OKE) with Ampere A1 Flex node pools. OCI also offers:
- **OCI Object Storage** with S3-compatible API (set `TRANSCODER_S3_ENDPOINT` to the OCI S3 compatibility endpoint)
- **OCI Cache with Redis** (managed Redis service)

The Kubernetes manifests are identical to the AWS EKS / GCP GKE patterns — only the container registry (`<region>.ocir.io`) and node instance shapes differ.

---

## 7.10 Geo-Scale Multi-Regional Production Architecture

For global deployment serving users on every continent, each region runs a **fully independent, self-contained stack**: Gateway + Coordinator + Workers + Redis + NATS + Etcd + S3. Regions do NOT share databases or queues. Cross-region concerns are handled at three layers:

### Layer 1: Client Routing — Anycast DNS / Latency-Based Routing

| Provider | Service | How It Works |
| :--- | :--- | :--- |
| Cloudflare | Magic Transit / Load Balancing | Anycast IPs route users to nearest PoP, then proxied to regional Gateway |
| AWS | Route 53 Latency-Based Routing | DNS resolves to the ALB in the region with lowest latency to the client |
| GCP | Global External HTTP(S) LB | Single anycast IP; Google's backbone routes to nearest GKE cluster |

A user in London is routed to `gateway.eu-west`. A user in New York is routed to `gateway.us-east`. Presigned upload URLs point to the **local** regional S3 bucket, keeping upload traffic within the region.

### Layer 2: Region-Prefixed Job IDs

Each Gateway tags jobs with a region prefix: `us-east:550e8400-...`, `eu-west:a37f2c00-...`. This ensures all Redis keys, NATS subjects, and S3 paths stay within the originating region's infrastructure. There are **no cross-region database locks** and **no cross-region NATS task routing**.

### Layer 3: Cross-Region Manifest Replication (CRR)

After a job completes, the final HLS/DASH manifests (`master.m3u8`, `manifest.mpd`) and metadata (`job_completed.json`, `job_manifest.json`) are replicated to other regions. **Raw video segments are NOT replicated** — they are served via CDN pull-through from the origin region.

| Deployment Style | CRR Mechanism |
| :--- | :--- |
| **AWS** | Native S3 Cross-Region Replication rules (automatic, asynchronous) |
| **Self-Hosted MinIO** | [`simulate_crr.go`](../scripts/simulate_crr.go) pattern: polls source MinIO every 2 seconds, copies manifests to destination MinIO |
| **CDN-Only** | No explicit replication; Cloudflare / CloudFront edge caches fetch from origin on first viewer request |

### Layer 4: NATS Gateway Super-Cluster (Optional)

For cross-region event streaming (e.g., a global admin dashboard showing jobs from all regions):

```yaml
# nats-us-east.conf
server_name: nats-us-east
listen: 0.0.0.0:4222
jetstream: { store_dir: /data/jetstream }
gateways:
  name: us-east
  port: 7222
  gateways:
    - name: eu-west
      urls: ["nats://nats-eu-west.vod.internal:7222"]
    - name: ap-southeast
      urls: ["nats://nats-ap-southeast.vod.internal:7222"]
```

---

## 7.11 Deploying the Frontend Applications

The VOD Engine ecosystem includes two frontend web applications: the **Developer Portal** (`developer-portal/`) and the **Admin Console** (`admin-console/`).

### 7.11.1 Admin Console Deployment ([`admin-console/`](../admin-console/))

The Admin Console is a Single Page Application (SPA) built with **Vite, React 19, TypeScript, and Tailwind CSS**.

#### 1. Local Development Mode (Vite Dev Server)
```bash
cd admin-console

# Install dependencies
npm install

# Start development server on default Vite port (or configured port :3001)
npm run dev
```
Open `http://localhost:5173` (or `http://localhost:3001`). Configure the Gateway API endpoint in the console header to point to `http://localhost:8080`.

#### 2. Production Build & Static Hosting
```bash
cd admin-console

# Typecheck and build optimized static assets into dist/
npm run build
```
The resulting `dist/` directory contains static HTML, JavaScript chunks, and CSS files.

**Deploying to NGINX / Docker**:
```dockerfile
# admin-console/Dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
# NGINX SPA fallback config (redirect all routes to index.html)
RUN echo 'server { listen 80; location / { root /usr/share/nginx/html; index index.html; try_files $uri $uri/ /index.html; } }' > /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
```

**Deploying to Cloudflare Pages / Vercel / Netlify**:
- **Framework Preset**: Vite
- **Build Command**: `npm run build`
- **Output Directory**: `dist`
- **Environment Variables**: `VITE_API_GATEWAY_URL=https://vod-api.yourdomain.com`

---

### 7.11.2 Developer Portal Deployment ([`developer-portal/`](../developer-portal/))

The Developer Portal is built with **Next.js 16 (App Router), React 19, Tailwind CSS v4, and `hls.js`**.

#### 1. Local Development Mode
```bash
cd developer-portal

# Install dependencies
npm install

# Start Next.js development server
npm run dev
```
Open `http://localhost:3000` in your browser.

#### 2. Production Node.js Server Deployment
```bash
cd developer-portal

# Build the Next.js production bundle
npm run build

# Start the Node.js production server on port 3000
npm run start
```

#### 3. Standalone Docker Container Deployment
Add `output: 'standalone'` to `developer-portal/next.config.ts`:

```dockerfile
# developer-portal/Dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
EXPOSE 3000
CMD ["node", "server.js"]
```

#### 4. Deploying to Vercel PaaS (Recommended)
Because Vercel is the creator of Next.js, deploying `developer-portal/` to Vercel takes seconds:
```bash
# Install Vercel CLI
npm install -g vercel

# Deploy from the developer-portal directory
cd developer-portal
vercel --prod
```
- **Environment Variable**: `NEXT_PUBLIC_GATEWAY_URL=https://vod-api.yourdomain.com`

---

## 7.12 Troubleshooting Runbook

| Symptom | Root Cause | Fix |
| :--- | :--- | :--- |
| `partition_count must be divisible by nats_shard_count` | `coordinator.partition_count` is not evenly divisible by `coordinator.nats_shard_count` | Ensure clean division. Docker config uses 4 ÷ 2 = 2 ✓. Production should use 1024 ÷ 4 = 256 ✓. |
| MinIO `Bucket does not exist` | The `minio-init` or `create-buckets` init container failed silently during startup | Run `docker compose up minio-init` or manually: `mc alias set myminio http://localhost:9000 minioadmin minioadmin && mc mb myminio/transcoder-docker` |
| Redis `CROSSSLOT` error | Redis keys for the same job are being hashed to different slots | All job keys MUST wrap the UUID in curly braces: `job:{550e8400-...}:status`. The content inside `{}` becomes the hash tag. |
| Worker `insufficient disk space` | Scratch directory has less than `min_disk_free_gb` (default 1 GB in Docker, 10 GB production) free | Prune stale `.ts` segment files from the scratch dir. In K8s, increase `emptyDir.sizeLimit`. |
| Railway container health check fails | Gateway bound to static `:8080` instead of Railway's dynamic `$PORT` | Set `TRANSCODER_LISTEN_ADDR=0.0.0.0:${PORT}` in Railway environment variables. |
| AWS S3 `SignatureDoesNotMatch` | Clock skew between pod and AWS, or `TRANSCODER_REGION` doesn't match bucket region | Sync NTP: `sudo timedatectl set-ntp true`. Ensure `TRANSCODER_REGION` matches S3 bucket region exactly (e.g. `us-east-1`). |
| GCS `403 Forbidden` | GCS HMAC Interoperability keys expired, or IAM role missing `storage.objectAdmin` | Regenerate HMAC keys: GCP Console → Storage → Settings → Interoperability. Verify IAM binding. |
| NATS `tls: bad certificate` | Worker's TLS client certificate not signed by the same CA as the NATS server | Re-issue client certs via `cert-manager` using the same `ClusterIssuer`. |
| CRR Simulator not replicating | `simulate_crr.go` cannot reach source or destination MinIO | Verify MinIO endpoints: US-East at `127.0.0.1:9000`, EU-West at `127.0.0.1:9010`. Check `logs/simulate-crr.log` for errors. |
| `docker compose up` hangs on `minio-init` | MinIO hasn't finished starting when `minio-init` tries to connect | The `depends_on` ensures ordering, but MinIO may need 5+ seconds. The init script has a `sleep 5` for this. Increase to `sleep 10` if on slow hardware. |
| Admin Console / Dev Portal `CORS Error` | Gateway `:8080` or MinIO `:9000` missing Access-Control-Allow-Origin headers | Gateway handles CORS automatically. For MinIO, run `mc cors set myminio/transcoder-docker cors.json` with `"AllowedOrigins":["*"]`. |
| Developer Portal HLS Player fails to play | MinIO bucket access policy is set to private instead of public | Run `mc anonymous set public myminio/transcoder-docker` so `hls.js` can fetch `.m3u8` and `.ts` files without S3 authentication headers. |
| Next.js build error `hls.js window is not defined` | `hls.js` instantiated during Server-Side Rendering (SSR) | Wrap player component in `dynamic(() => import(...), { ssr: false })` or use client-side `useEffect` initialization. |


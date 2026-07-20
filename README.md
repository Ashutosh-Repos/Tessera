# 🎬 Tessera — Distributed Video-on-Demand (VOD) Engine

[![Go Version](https://img.shields.io/github/go-mod/go-version/Ashutosh-Repos/Tessera)](https://golang.org)
[![License](https://img.shields.io/github/license/Ashutosh-Repos/Tessera)](LICENSE)
[![Messaging](https://img.shields.io/badge/Messaging-NATS%20JetStream%20%7C%20SQS-blue)](https://nats.io)
[![State](https://img.shields.io/badge/State-Redis%20Cluster-red)](https://redis.io)
[![Consensus](https://img.shields.io/badge/Consensus-Etcd-lightblue)](https://etcd.io)
[![Storage](https://img.shields.io/badge/Storage-S3%20%2F%20MinIO-orange)](https://min.io)

Tessera is a cloud-agnostic, multi-region video ingestion and distributed transcoding engine built for global-scale platforms. It serves as an enterprise-grade, open-source alternative to AWS Elemental MediaConvert and Bitmovin. By eliminating per-minute transcoding fees, Tessera is designed to run on your own compute infrastructure, scaling dynamically from a **developer sandbox (10K users)** on a single VM to a **global-scale network (50M+ active users)** running on clustered Kubernetes pools.

---

## 📐 Architecture Overview

Tessera uses a stateless, shared-nothing architecture divided into three discrete layers:

```
                  ┌──────────────────────────────┐
                  │      Client Browser/App      │
                  └──────────────┬───────────────┘
                                 │
            ┌────────────────────┼───────────────────┐
            │ 1. Create Session  │ 2. Direct Upload  │ 3. Stream SSE
            ▼                    ▼                   ▼
     ┌─────────────┐      ┌─────────────┐     ┌─────────────┐
     │ Gateway API │      │   S3/MinIO  │     │ Gateway API │
     └──────┬──────┘      └──────▲──────┘     └──────▲──────┘
            │                    │                   │
            │ Publish            │ Read/Write        │ Pull Status
            ▼                    │                   │
     ┌─────────────┐             │            ┌──────┴──────┐
     │  NATS/SQS   │ ◄───────────┼───────────►│ Redis State │
     └──────┬──────┘             │            └──────▲──────┘
            │                    │                   │
            │ Consume Task       │ Write Chunks      │ Completion Pipeline
            ▼                    │                   │
     ┌─────────────┐             │            ┌──────┴──────┐
     │ Worker Fleet├─────────────┘            │ Coordinator │
     └────────────────────────────────────────┴─────────────┘
```

1. **Gateway (Stateless Edge)**: Authenticates sessions, generates secure upload tokens, and handles SSE telemetry. It runs in a zero-bandwidth configuration by keeping video bytes entirely out of the Gateway.
2. **Coordinator (Control Plane)**: Manages consistent hash ring topology via Etcd, detects video layouts, orchestrates faststart stream slicing, and compiles final HLS/DASH streaming manifests.
3. **Worker Fleet (Compute plane)**: Subscribes to prioritized task queues, pulls 5-second raw chunks from S3, executes isolated FFmpeg processes, and commits results atomically.

---

## ⚡ Key Architectural Innovations

### 1. Zero-Bandwidth API Gateway
Traditional gateways ingest video bytes directly, creating network bottlenecks and massive data transit fees. Tessera uses secure Multipart uploads. The Gateway validates client requests, requests a upload session from S3, and returns cryptographically signed presigned PUT URLs directly to the client. Client browsers PUT video binary parts **directly to S3/MinIO**. The Gateway only processes lightweight control-plane JSON (~5KB per connection).

### 2. Faststart Stream Slicing
Instead of downloading a massive 50GB file to local disk before slicing, the Coordinator fetches only the **first 1MB** of the video object to detect the position of the MP4 `moov` container atom:
* **Faststart** (`moov` before `mdat`): Pipes the S3 socket stream directly into `ffmpeg -i pipe:0 -f segment` in memory, writing keyframe-aligned 5-second raw segment chunks directly back to S3 with **zero disk usage**.
* **Non-Faststart**: Downloads the file once, remuxes with `-movflags +faststart` to relocate the atom, and then slices.

### 3. Consensus Hash Ring & Topology
To distribute slicing and manifest compilation jobs deterministically without database polling bottlenecks:
* Coordinators register with Etcd consensus using a 5-second lease TTL.
* A virtual-node hash ring (150 virtual nodes per coordinator instance mapped across 1024 partitions) routes jobs deterministically.
* Adding/removing instances triggers instant partition rebalancing.
* **Epoch Fencing** protects manifest writes: Coordinators validate ownership epochs in Redis, preventing stale coordinators from overwriting active playlists.

### 4. Single-RTT Redis Completion Pipeline
When a worker finishes a segment, it executes 5 critical operations in a single roundtrip via Redis Pipeline:
1. Marks task complete (`SET` with 24h TTL)
2. Updates completion bitmap (`SETBIT` at segment/resolution index)
3. Increments completion count (`HINCRBY`)
4. Stores segment duration (`HSET`)
5. Emits progress event (`XADD` to progress stream)

### 5. Progress Multiplexer
SSE streams usually require one Redis connection per client. Tessera's gateway runs a single Redis `XREAD BLOCK` loop that listens to all active progress streams and fans out updates to subscriber channels. This collapses 50,000 concurrent client connections into **1 Redis connection per gateway node**.

---

## 🛡️ Reliability & OS Guardrails

* **cgroups v2 Process Isolation**: On Linux, each FFmpeg subprocess is placed in an isolated cgroup, limiting memory to 1.5GB and lowering CPU scheduling weight to 50 to prevent noisy neighbor patterns. macOS falls back to `renice +10`.
* **Two-Tier Idempotency**: Workers use a Redis `EXISTS` fast-path check (<0.1ms). If Redis is unreachable, a circuit breaker trips to a secure S3 `HeadObject` fallback.
* **NATS/SQS DLQ with Exponential Backoff**: Failed segments land in a Dead Letter Queue. Coordinators apply exponential delays ($10\text{s} \times 2^{\text{retries}-1}$) before re-queueing tasks.
* **Process Watchdogs**: Active disk pre-flights (`syscall.Statfs`), temp file size watchers (`SIGKILL` if temp files exceed 3GB), and task execution duration timeouts (5 mins).
* **Hash Tag Routing**: To prevent cross-slot errors in Redis Cluster, all keys for a specific job include the Job ID wrapped in curly braces (e.g. `job:{jobID}:status`), forcing them to land on the same cluster node.

---

## 📊 Capacity Sizing & Cost Economics

Tessera is built to run on standard VMs and scales sub-linearly: as volume increases, compute cost scales horizontally while per-minute licensing remains at zero.

### Monthly Sizing & Cost Comparison (vs. AWS MediaConvert)

| Scale Tier | Concurrent Peak | VM Instances (AWS equivalent) | Sizing Config | Est. Tessera Cost/mo | AWS MediaConvert Cost/mo* | Net Savings |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Tier 1 (Developer)** | 1–3 concurrent | 1× `t3.xlarge` (4 vCPUs, 16GB RAM) | 8 partitions, 1 NATS shard | **~$75** | ~$450 | **83%** |
| **Tier 2 (Startup)** | ~10 concurrent | 2× VMs (`c6i.xlarge` + `m6i.xlarge`) | 32 partitions, 2 NATS shards | **~$200** | ~$1,350 | **85%** |
| **Tier 3 (Growth)** | ~20 concurrent | 10× VMs (2 GW, 2 Coord, 3 Worker, 2 Redis, 1 NATS, 2 MinIO) | 64 partitions, 2 NATS shards | **~$800** | ~$3,600 | **78%** |
| **Tier 4 (Decoupled)** | 10–30 concurrent | 16× VMs (includes 3 Sentinel, 4 NVMe MinIO nodes) | 128 partitions, 4 NATS shards | **~$2,000** | ~$9,000 | **78%** |
| **Tier 5 (Enterprise)** | 300–500 peak | 40+ VMs (16× GPU `g4dn.xlarge`, Redis Cluster, Ceph) | 512 partitions, 16 NATS shards | **~$15,000** | ~$54,000 | **72%** |
| **Tier 6 (Global)** | 50,000+ peak | 120× GPU `g5.4xlarge` + 60 infra VMs per region | 1024 partitions, 64 NATS shards | **~$100K/region** | $500K+ | **80%** |

*\*AWS MediaConvert estimate based on standard quality tier ($0.024/min average) with 5-minute average video length.*

### Hardware Acceleration Economics

Tessera supports multiple GPU acceleration backends (`nvenc`, `vaapi`, `videotoolbox`), speeding up transcode times and dropping compute costs:

| Encoder Mode | FFmpeg Codec | Speed vs CPU | Cost per 1-hour video (3 resolutions) |
| :--- | :--- | :--- | :--- |
| **CPU (libx264 fast)** | `libx264` | 1× (12 min transcode) | ~$0.18 (c6i.xlarge VM time) |
| **NVIDIA NVENC** | `h264_nvenc` | 6–8× (2 min transcode) | ~$0.04 (g4dn.xlarge GPU VM time) |
| **Intel VAAPI** | `h264_vaapi` | 3–4× (3.5 min transcode) | ~$0.08 |
| **Apple VideoToolbox** | `h264_videotoolbox` | ~4× | Dev/local only (M-series Silicon) |

### Network Savings: Manifest-Only CRR
Cross-region replication (CRR) can easily explode your network egress bill. Tessera implements **Manifest-Only CRR**: heavy `.ts` segments stay local to the region that transcoded them, while only tiny playlist manifests (`master.m3u8`, `manifest.mpd`) and metadata (~10KB total) are replicated. This results in **>99.99% WAN bandwidth cost savings**.

---

## 🚀 Quickstart (Platform-in-a-Box)

Spin up the full distributed cluster locally in **60 seconds**:

```bash
# 1. Clone the repository
git clone https://github.com/Ashutosh-Repos/Tessera.git
cd Tessera

# 2. Start the cluster (Docker Compose infra + Go engines)
chmod +x start.sh && ./start.sh
```

### Endpoints Available Immediately:
* **API Gateway**: `http://localhost:8080` (CORS-enabled REST edge)
* **S3 Object Storage Console**: `http://localhost:9001` (Credentials: `minioadmin` / `minioadmin`)
* **Developer Customizer Portal**: `http://localhost:3000` (`cd developer-portal && npm run dev`)
* **SRE Admin Console**: `http://localhost:5173` (`cd admin-console && npm run dev`)

### Scaling Worker Compute:
```bash
# Scale worker compute daemons up on the fly to process larger queue backlogs
docker compose -f docker-compose.prod.yml scale worker=5

# View real-time transcoding output logs from the worker fleet
docker compose -f docker-compose.prod.yml logs -f worker
```

---

## 📖 Unified Documentation

Developer documentation is split into clean, high-density reference files:

* **[Core Architecture & Design](docs/architecture.md)** — Detailed overview of the 3-tier partitioning, consistent hash ring consensus, slicing flow, failover guards, and trace correlation.
* **[Developer Integration Guide](docs/integration_guide.md)** — Step-by-step REST API reference (create session, get presigned URL batch, complete upload, SSE telemetry) and React `ui-sdk` widgets.
* **[Production Deployment & Sizing](docs/deployment.md)** — Complete environment variables catalog, local multi-region simulation settings, and capacity sizing matrix (Tiers 1 to 6).
* **[Architecture Decisions (ADRs)](docs/adr.md)** — Record of technical decisions and trade-offs (SSE vs WebSockets, Hash Ring vs Central Queue, FFmpeg subprocess vs CGo).
* **[Complete Architecture, Logic & Metrics Analysis](docs/analysis_results.md)** — Comprehensive analysis of design logic, production readiness evaluation, hardware/cost efficiency, and complete Prometheus metrics registry.
* **[Free Cloud Setup & Benchmarking Guide](docs/cloud_benchmarking.md)** — Operational playbook for zero-cost cloud deployments, automated load-testing scripts, and benchmarking scorecards across OCI, GCP, and GPU platforms.

---

## 📂 Repository Layout

* `cmd/transcoder/` — Single CLI entrypoint for gateway, coordinator, and worker modes.
* `internal/` — Core Go engines (hash ring, slicer, worker executors, NATS/Redis/Etcd/SQS infra drivers).
* `ui-sdk/` — Custom visual component package containing VideoPlayer and VideoTile.
* `developer-portal/` — Next.js visual customization studio.
* `admin-console/` — Vite + React 19 visual telemetry monitor dashboard.

---

## 🤝 Contributing & Community

We warmly invite contributors to help push Tessera forward!

### Active Development Areas:
1. **Infrastructure Drivers (Roadmap V2)**:
   * Implement the Google Cloud Pub/Sub driver using `cloud.google.com/go/pubsub`.
   * Implement the Kafka driver (compatible with AWS MSK, OCI Streaming, or self-hosted Kafka) using pure-Go `github.com/segmentio/kafka-go` to avoid heavy CGo dependencies.
   * For design blueprints and implementation steps, read the **[V2 Cloud Message Bus Proposal](next%20version/idea1.md)**.
2. **GPU Optimization**: Help profile or add support for extra GPU hardware-accelerated profiles.
3. **Web UI Customization**: Improve dashboard layout, telemetries, or player seek aesthetics in `admin-console` or `developer-portal`.

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.

# 🎬 Tessera — Distributed Video-on-Demand (VOD) Engine

[![Go Version](https://img.shields.io/github/go-mod/go-version/Ashutosh-Repos/Tessera)](https://golang.org)
[![License](https://img.shields.io/github/license/Ashutosh-Repos/Tessera)](LICENSE)
[![Messaging](https://img.shields.io/badge/Messaging-NATS%20JetStream-blue)](https://nats.io)
[![State](https://img.shields.io/badge/State-Redis%20Cluster-red)](https://redis.io)
[![Consensus](https://img.shields.io/badge/Consensus-Etcd-lightblue)](https://etcd.io)
[![Storage](https://img.shields.io/badge/Storage-S3%20%2F%20MinIO-orange)](https://min.io)

Tessera is a cloud-agnostic, multi-region video ingestion and distributed transcoding engine built for global-scale platforms. It serves as an enterprise, open-source alternative to AWS Elemental MediaConvert and Bitmovin, designed to scale dynamically from **10K sandbox users** on a single VM to **50M+ active users** on clustered Kubernetes pools.

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

---

## 🛠️ The Developer Dilemma: Video Is Hard & Expensive

Adding video uploads and streaming to your SaaS or social app typically leads to three major roadblocks:

1. **Buffering & Device Incompatibility**: Raw videos (MP4, MOV) buffer heavily over mobile networks. You need to convert them into adaptive bitrate formats (HLS/MPEG-DASH) so the quality adapts dynamically.
2. **Exorbitant Per-Minute Billing**: AWS MediaConvert and Bitmovin charge per-minute licensing fees that scale out of control as your traffic grows.
3. **The Single-Server FFmpeg Crash**: DIY transcoder scripts on a single server will lock up the CPU, leak memory, run out of disk space, or corrupt manifests when multiple users upload at the same time.

---

## ⚡ How Tessera Solves It: Core Architecture Highlights

Tessera is a **decoupled, shared-nothing distributed engine** written in Go that runs on your own VMs or Kubernetes pools. You only pay for raw compute.

### 1. Zero-Bandwidth API Gateway

Traditional gateways ingest video bytes directly, starving control plane traffic. Tessera generates cryptographically signed AWS S3/MinIO PUT URLs. Clients upload multi-part binary chunks **directly to object storage**, keeping API gateway network footprint at zero.

### 2. Faststart Stream Slicing

Instead of downloading a massive 50GB file to disk before slicing, the Coordinator queries S3 and reads the first **1MB** of the object to locate the MP4 `moov` container atom.

- If it's a Faststart layout (`moov` before `mdat`), it pipes the S3 socket stream directly into `ffmpeg -i pipe:0 -f segment` in memory, writing 5-second raw segment chunks to S3.
- Fragmented layout files are downloaded once, remuxed with `-movflags +faststart`, and then sliced.

### 3. Consensus Hash Ring & Partitioning

To distribute slicing jobs deterministically without database polling bottlenecks:

- Coordinators register in Etcd under a 5s lease TTL. The ring assigns 150 virtual nodes per Coordinator across 1024 partitions.
- Adding or removing coordinator instances triggers instant rebalances.
- Split-brains are fenced out using **Epoch Fencing**—Coordinators validate partition owners in Redis and abort compilation if `storedEpoch > currentEpoch`.

### 4. Ephemeral Redis Hash Tag Routing

To guarantee atomic operations in Redis Cluster without encountering fatal `CROSSSLOT` errors, all keys for a specific job include the JobID wrapped in curly braces (e.g. `job:{jobID}:status`, `progress:{jobID}`). This ensures that CRC16 maps all keys associated with a single job to the exact same Redis Cluster node.

### 5. Resiliency-by-Design

- **Idempotency Bitsets**: Workers check Redis completion bitsets (or S3 `HeadObject` fallbacks) before starting FFmpeg to skip duplicate NATS tasks.
- **NATS DLQ Backoffs**: Failed tasks are routed to `transcode-tasks-dlq`. Coordinators apply exponential delays ($10\text{s} \times 2^{\text{retries}-1}$) before republishing.
- **OS Resource Watchdogs**: Active disk pre-flights (`syscall.Statfs`), temp file size watchers (`SIGKILL` on process groups), and task timeouts (5 mins).

---

## 🚀 Quickstart (Platform-in-a-Box)

Run the full distributed cluster locally on your machine in **60 seconds**:

```bash
# 1. Clone the repository
git clone https://github.com/Ashutosh-Repos/Tessera.git
cd Tessera

# 2. Start the cluster (Docker Compose infra + Go engines)
chmod +x start.sh && ./start.sh
```

### Endpoints Available Immediately:

- **API Gateway**: `http://localhost:8080`
- **S3 Object Storage Console**: `http://localhost:9001` (Credentials: `minioadmin` / `minioadmin`)
- **Developer Customizer Portal**: `http://localhost:3000` (`cd developer-portal && npm run dev`)
- **SRE Admin Console**: `http://localhost:5173` (`cd admin-console && npm run dev`)

### Common Developer/SRE Docker Operations:

```bash
# Scale worker compute daemons up on the fly
docker compose -f docker-compose.prod.yml scale worker=5

# View real-time logs from the worker fleet
docker compose -f docker-compose.prod.yml logs -f worker

# Tear down the cluster and wipe volumes
docker compose -f docker-compose.prod.yml down -v
```

---

## 📖 Unified Documentation

Developer documentation is split into clean, high-density reference files:

- **[Core Architecture & Design](docs/architecture.md)** — In-depth overview of the 3-tier partitioning, consistent hash ring consensus, slicing flow, failover guards, and trace correlation.
- **[Developer Integration Guide](docs/integration_guide.md)** — Step-by-step REST API reference (create session, get presigned URL batch, complete upload, SSE telemetry) and React `ui-sdk` widgets.
- **[Production Deployment & Sizing](docs/deployment.md)** — Complete environment variables catalog, local multi-region simulation settings, and capacity sizing matrix (Tiers 1 to 6).
- **[Architecture Decisions (ADRs)](docs/adr.md)** — Record of technical decisions and trade-offs (SSE vs WebSockets, Hash Ring vs Central Queue, FFmpeg subprocess vs CGo).

---

## 📂 Repository Layout

- `cmd/transcoder/` — Single CLI entrypoint for gateway, coordinator, and worker modes.
- `internal/` — Core Go engines (hash ring, slicer, worker executors, NATS/Redis/Etcd infra drivers).
- `ui-sdk/` — Custom visual component package containing VideoPlayer and VideoTile.
- `developer-portal/` — Next.js visual customization studio.
- `admin-console/` — Vite + React 19 visual telemetry monitor dashboard.

---

## 🤝 Contributing & Community

We warmly invite contributors from all corners of the globe to help push this platform forward!

Whether you want to **raise an issue**, **suggest a new feature**, **refine the docs**, or **submit a pull request**, your help is highly appreciated. Let's collaborate to build the ultimate open-source, multi-cloud video engine and make it highly accessible and usable for the world.

### Active Development Areas:

1. **Infrastructure Drivers (Roadmap V2)**:
   - Implement the Google Cloud Pub/Sub driver using `cloud.google.com/go/pubsub`.
   - Implement the Kafka driver (compatible with Oracle Cloud OCI Streaming, Amazon MSK, and self-hosted Kafka) using pure-Go `github.com/segmentio/kafka-go` to avoid heavy CGo dependencies.
   - For design blueprints and implementation steps, read the **[V2 Cloud Message Bus Proposal](next%20version/idea1.md)**.
2. **GPU Optimization**: Help profile or add support for extra GPU hardware-accelerated profiles.
3. **Web UI Customization**: Improve dashboard layout, telemetries, or player seek aesthetics in `admin-console` or `developer-portal`.
4. **Cloud Provider Adaptations**: Add deployment templates, terraform modules, or configuration sizing matrices for additional cloud providers (e.g. Oracle Cloud, GCP, Azure, bare-metal).

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.

# 🎬 Tessera — Distributed Video-on-Demand (VOD) Engine

## 📖 Introduction
Tessera is a cloud-agnostic, multi-region video ingestion and distributed transcoding engine built for global-scale platforms. It is designed as an enterprise-grade, open-source alternative to AWS Elemental MediaConvert and Bitmovin.

---

## ⚠️ The Problem: Why It Exists
Processing massive video uploads for millions of concurrent users globally introduces significant system-level bottlenecks:
1. **High WAN Ingress Costs**: Transferring multi-gigabyte raw video uploads across global regions or centralizing them in a single cloud bucket is extremely expensive and slow.
2. **Gateway Ingestion Bottleneck**: Forcing heavy video payloads to stream through an API gateway node saturates its network interfaces, causing scale limits and slow uploads.
3. **Split-Brain & Duplicate Work**: Without distributed locking, multiple nodes can slice the same file or transcode the same segments multiple times, wasting costly compute resources.
4. **Slow Slicing Turnaround**: Slicing large source videos usually requires downloading the entire file to a local temp disk to parse video indexes, introducing disk bottlenecks and latency.

---

## ✅ What Is Solved
Tessera resolves these scaling issues through a 3-tier, share-nothing regional architecture:
1. **Direct-to-S3 Ingestion & Data Gravity**: Gateways issue secure JWT session claims allowing client browsers to upload chunks directly to local regional buckets. Massive media payloads remain local to their home region.
2. **Manifest-Only Cross-Region Replication (CRR)**: Regional accessibility is achieved by replicating *only* lightweight HLS playlists (`.m3u8`) and DASH manifests (`.mpd`) across regions. Heavy video chunks never cross WAN lines.
3. **Consensus Hash Ring & Atomic Fencing**: A 1024-slot consistent hash ring backed by etcd leases dynamically distributes partition managers and uses epoch fencing to prevent duplicate task execution.
4. **Faststart GOP-Aligned Slicing**: Slicers query raw 1MB video atoms via S3 HTTP range requests, slicing 50GB source files into GOP-aligned segment tasks in **<500ms** without downloading source files.
5. **Sharded Task Pipelines**: Elastic workers pull segments from sharded regional NATS JetStream queues, execute GPU-accelerated FFmpeg transcodes, and perform atomic bitmap status commits in Redis.

---

## 🚀 Quickstart (Platform-in-a-Box)

Run the full distributed cluster locally on your machine in **60 seconds**:

```bash
# 1. Clone the repository
git clone https://github.com/Ashutosh-Repos/Tessera.git
cd Tessera

# 2. Boot the single-region platform (Docker Compose)
chmod +x start.sh && ./start.sh
```

### Endpoints Available Immediately:
- **API Gateway**: `http://localhost:8080`
- **S3 Object Storage Console**: `http://localhost:9001` (Credentials: `minioadmin` / `minioadmin`)
- **Developer Customizer Portal**: `http://localhost:3000` (`cd developer-portal && npm run dev`)
- **SRE Admin Console**: `http://localhost:5173` (`cd admin-console && npm run dev`)

---

## 📖 System Documentation

Detailed design patterns, structural architecture diagrams, and deployment guides are available in the [`docs/`](docs/) directory:

- [Chapter 1: Introduction & Goals](docs/01-introduction-and-goals.md) — Quality goals and constraints.
- [Chapter 2: Architecture Constraints](docs/02-architecture-constraints.md) — Technical, organizational, and convention limits.
- [Chapter 3: Context & Scope](docs/03-context-and-scope.md) — C4 context diagrams and external integrations.
- [Chapter 4: Solution Strategy](docs/04-solution-strategy.md) — Core technical decisions and tradeoffs.
- [Chapter 5: Building Block View](docs/05-building-block-view.md) — C4 component deep-dive.
- [Chapter 6: Runtime View](docs/06-runtime-view.md) — Dynamic execution trajectory sequence diagrams.
- [Chapter 7: Deployment View](docs/07-deployment-view.md) — Multi-cloud production deployment strategies (AWS, GCP, OCI).
- [Chapter 8: Cross-Cutting Concepts](docs/08-cross-cutting-concepts.md) — Observability, telemetry, tracing, and security.
- [Chapter 9: Architectural Decisions](docs/09-architecture-decisions.md) — ADR log tracking design changes.
- [Deployment Sizing Tiers Manual](docs/deployment_scaling_tiers.md) — Comprehensive guide for hardware capacity planning.

---

## 📂 Repository Layout

- `cmd/transcoder/` — Single unified CLI entrypoint for gateway, coordinator, and worker modes.
- `internal/` — Core Go engines (consistent hash ring, slicing orchestrator, worker executors, S3/Redis/NATS/etcd infra drivers).
- `ui-sdk/` — Custom visual component package containing VideoPlayer and VideoTile.
- `developer-portal/` — Next.js visual customization studio.
- `admin-console/` — Vite + React 19 visual telemetry monitor dashboard.

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.

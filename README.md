# 🎬 Tessera — Distributed Video-on-Demand (VOD) Engine

Tessera is a hyper-scalable, cloud-agnostic, multi-region video ingestion and distributed transcoding engine built for global scale. It serves as an enterprise, cloud-native open-source alternative to AWS Elemental MediaConvert and Bitmovin.

---

## ✨ Key Features

- **⚡ Faststart MP4 Stream Slicing**: Instantly slices raw videos into GOP-aligned chunk tasks without downloading the full source file first.
- **🛡️ Consensus Hash Ring**: Leverages consistent hashing backed by Etcd leases to balance jobs across a coordinator ring and prevent duplicate work.
- **🚀 Pull-Based Worker Fleet**: Elastic workers pull tasks from NATS JetStream, execute FFmpeg hardware/software transcoding, and commit results.
- **📡 Real-Time Progress Telemetry**: Emits live progress streams and rich asset metadata directly to connected web clients via WebSockets and SSE.
- **🌐 Geo-Scale Multi-Region Isolation**: Fully isolated compute regions (e.g., US-East, EU-West) with manifest-only Cross-Region Replication (CRR).
- **💻 Complete Web Ecosystem**: Features a Next.js Developer Portal (Studio + Player SDK) and a React Admin Console for SRE telemetry.

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

# 🎬 Tessera — Distributed Video-on-Demand (VOD) Engine

## 📖 Introduction
Tessera is a cloud-agnostic, multi-region video ingestion and distributed transcoding engine built for global-scale platforms. It is designed as an enterprise-grade, open-source alternative to AWS Elemental MediaConvert and Bitmovin.

---

## ⚠️ The Real-World Problems

Processing high-volume video uploads globally for millions of users introduces severe physical and financial challenges:

1. **💸 Astronomical Cloud Network Bills**: Transferring raw, multi-gigabyte video uploads across regional data centers or centralizing them in a single global cloud bucket incurs massive network ingress and egress costs.
2. **🔌 Gateways Crash During Peak Uploads**: When thousands of users upload high-resolution video clips simultaneously (such as during a live event or breaking news), ingress API gateways saturate their network ports, drop connections, and crash.
3. **💰 Wasted Compute Costs (Duplicate Work)**: Without strict distributed coordination, worker nodes in a cluster can end up transcoding the same video segments multiple times due to node failovers or split-brain states, driving up server bills.
4. **⏳ Hours of Delay to Begin Transcoding**: Traditional systems must download a multi-gigabyte source video to a coordinator's local disk to analyze its structure and index metadata before they can slice it, introducing massive disk IO bottlenecks and starting delays.

---

## ✅ What Is Solved

Tessera solves these real-world scaling and cost issues through an optimized, share-nothing regional model:

1. **Zero Global WAN Payload Traffic**: Videos are ingested and processed entirely within their local Point of Presence (PoP) region. Only the lightweight, kilobyte-sized manifest playlists (`.m3u8` / `.mpd`) are replicated globally, saving massive network bandwidth.
2. **Line-Rate Upload Resilience**: The API Gateway only handles session orchestration and lightweight metadata. Clients upload video parts directly to scalable object storage (like AWS S3 or Ceph), preventing gateway chokepoints.
3. **Guaranteed Zero-Duplication Compute**: A consistent hash ring backed by etcd consensus leases and atomic epoch fencing guarantees that exactly one coordinator node coordinates task distribution, ensuring 100% compute cost efficiency.
4. **Sub-Second Slicing Initiation**: Instead of downloading raw files, the engine queries the exact 1MB index byte offsets (`moov`/`mdat` atoms) over HTTP range requests, slicing 50GB source videos into parallel transcoding chunks in **less than 500ms**.

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

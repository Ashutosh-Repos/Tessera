# 🎬 Tessera — Distributed Video-on-Demand (VOD) Engine

## 📖 Introduction
Tessera is a cloud-agnostic, multi-region video ingestion and distributed transcoding engine built for global-scale platforms. It is designed as an enterprise-grade, open-source alternative to AWS Elemental MediaConvert and Bitmovin.

---

## ⚠️ The Real-World Problem: Adding Video Is Hard & Expensive

Whether you are building a streaming app (like Netflix), a short-form video feed (like TikTok), a course platform, or just adding video uploads to an existing SaaS product, you run into three major roadblocks:

1. **Buffering & Slow Loads**: Standard raw video files (like `.mp4`) do not stream well on web or mobile. You need to convert them into adaptive bitrate formats (HLS/DASH) so the quality adjusts dynamically depending on the user's connection.
2. **Exorbitant Billing (AWS Elemental Lock-in)**: Commercial transcoding APIs (like AWS MediaConvert or Bitmovin) charge per-minute fees that scale out of control as your user base and video volume grow.
3. **The "Single-Server" Crash**: Building a DIY transcoder script using FFmpeg on a single server works for a few files. But if 50 users upload files at the same time, the server runs out of CPU/memory, files get corrupted, and your application crashes. 
4. **Complex Cluster Synchronization**: Trying to split a video into segments to process them in parallel across a group of servers requires writing complex distributed lock managers, leading to race conditions and duplicate transcode bills.

---

## ✅ How Tessera Solves It

Tessera is an **open-source, self-hosted alternative** to AWS MediaConvert. It lets you run a highly available, distributed video pipeline on your own hardware:

1. **Instant Adaptive Playback (HLS/DASH)**: Automatically processes raw video uploads into responsive HLS streams with multi-quality resolution playlists (1080p, 720p, 480p) ready for standard players like `hls.js`.
2. **Zero Per-Minute Licensing Fees**: Run the transcoder on your own VMs (AWS EC2, GCP, Oracle Cloud, or bare metal). You only pay for your raw compute resources—no commercial transcoding markups.
3. **Parallel Sub-Second Slicing**: Instead of taking hours to transcode a long video, Tessera slices files into parallel chunk tasks in **under 500ms** by reading raw header indices directly over S3 HTTP range requests.
4. **Infinite Horizontal Scale**: Compute is distributed across an elastic worker fleet. Slicing coordinators organize tasks on a consensus hash ring, ensuring that workers never step on each other or double-process the same file segments.

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

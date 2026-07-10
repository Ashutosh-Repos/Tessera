# 1. Introduction and Goals

**Tessera** is an open-source, cloud-agnostic video transcoding platform. It accepts raw video uploads, slices them into parallel segments, transcodes each segment across a fleet of workers, and compiles the output into HLS and DASH adaptive bitrate playlists. It is a self-hosted alternative to AWS Elemental MediaConvert, Bitmovin, and GCP Transcoder API.

Raw video files uploaded from phones, cameras, or screen recorders are large (often multi-gigabyte) and not suitable for direct streaming. Serving them as-is causes buffering, high bandwidth costs, and a poor playback experience on slower connections.

The standard solution is **Adaptive Bitrate Streaming (ABR)** — protocols like HLS and DASH. ABR works by converting a source video into multiple resolution variants (1080p, 720p, 480p), each split into short segments (5 seconds each). The player then picks the best quality based on the viewer's network speed.

Traditional transcoding setups process the whole file on a single machine. This means long processing times for large videos, big local disk requirements, and if the machine fails mid-job, all progress is lost.

Tessera solves this with a **3-tier architecture** (Gateway → Coordinator → Worker). The Gateway handles upload sessions and issues presigned S3 URLs so clients upload directly to storage. The Coordinator slices the uploaded file into 5-second chunks and fans out transcoding tasks across NATS queues. Workers pull tasks, run FFmpeg, upload the result, and mark completion in Redis. Once all segments are done, the Coordinator compiles HLS/DASH playlists.

---

## 1.1 Core System Capabilities & Architectural Principles

The engine is engineered around five fundamental capabilities that distinguish it from legacy transcoding platforms:

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                                     Tessera Core                                         │
├──────────────────────────┬────────────────────────────────────┬──────────────────────────┤
│ Ingestion Layer          │ Execution & Slicing Layer          │ Delivery & State Layer   │
│ • Direct S3 Presigned    │ • 1MB Faststart Stream Slicing     │ • Redis Pipeline State   │
│ • 50GB File Handling     │ • Keyframe-Aligned Segments        │ • SSE Multiplexing       │
│ • Zero Gateway Bandwidth │ • Idempotent Bitset Skipping       │ • Epoch-Fenced Manifests │
└──────────────────────────┴────────────────────────────────────┴──────────────────────────┘
```

### 1. Stateless Gateway Presigned Ingestion
Traditional API gateways ingest file uploads through HTTP request bodies, forwarding bytes to application servers. For large video files, this creates bandwidth bottlenecks. Tessera eliminates this through a **Stateless Presigned Upload Flow**. The Gateway generates signed S3/MinIO PUT URLs for multi-part uploads. The client uploads directly to object storage, keeping Gateway bandwidth at zero and ensuring the API layer stays responsive regardless of upload volume.

### 2. Faststart Stream Slicing
Traditional setups download the entire source file to local disk before they can slice it. Tessera avoids this full download by reading the first **1MB** of the S3 object to check for the MP4 `moov` atom ([`slicer.go:L104-112`](../internal/coordinator/slicer.go#L104)). If the `moov` atom appears before `mdat` (faststart layout), the Coordinator pipes the S3 stream directly into `ffmpeg -i pipe:0`, which segments it into 5-second chunks written to a temporary directory. Those chunks are then uploaded to S3 and the temp directory is cleaned up. If the file is not faststart, it's downloaded once, remuxed with `-movflags +faststart`, and then sliced.

### 3. Lock-Free Worker Idempotency
NATS JetStream and SQS both guarantee at-least-once delivery, which means workers can receive the same task twice. To avoid wasting GPU/CPU time on a segment that's already done, the worker runs a two-tier idempotency check ([`executor.go:L56`](../internal/worker/executor.go#L56)). First, it asks Redis whether the task already exists (fast path, sub-millisecond). If Redis is down (circuit breaker is open), it falls back to an S3 `HeadObject` check on the output key (slow path). If either confirms the segment is already transcoded, the message is immediately ACKed and skipped.

### 4. Real-Time Progress Stream Multiplexing
The Gateway runs a single background goroutine (the **Progress Multiplexer**, [`multiplexer.go`](../internal/gateway/multiplexer.go)) that issues a blocking `XREAD BLOCK` against Redis Streams for active jobs. When a progress event arrives, it's fanned out to all subscribed SSE client channels. A non-blocking channel send (`select { case ch <- update: default: }`) ensures that a slow client connection never blocks the multiplexer or leaks memory — if the channel is full, the update is simply dropped for that client.

### 5. Epoch-Fenced Manifest Compilation
If a Coordinator crashes and a new one takes over the partition, the old Coordinator might wake up and try to compile playlists for the same job. To prevent this, every Coordinator maintains a monotonic `currentEpoch` counter that increments on each ring rebalance. Before compiling manifests, the code checks the epoch stored in Redis against its own ([`manifest.go:L28-35`](../internal/coordinator/manifest.go#L28)). If `storedEpoch > currentEpoch`, the Coordinator knows it's stale and aborts the compile. This prevents two Coordinators from writing conflicting `master.m3u8` files.

---

## 1.2 Quality Goals (SLAs & Architectural Drivers)

The architecture is governed by four primary quality goals, ordered by strict priority:

| Priority | Quality Goal | Target Metric / SLA | Architectural Realization & Technical Guarantees |
| :--- | :--- | :--- | :--- |
| **P1** | **Cloud Agnosticism** | 100% Driver Swappability | All storage, state, and event bus operations are strictly isolated behind Go interfaces ([`StateStore`](../internal/infra/store.go#L11), [`ObjectStore`](../internal/infra/s3.go#L18), [`MessageBus`](../internal/infra/bus.go#L9)). The platform can switch from NATS to AWS SQS or MinIO to S3 by changing a single YAML flag. |
| **P2** | **Extreme Scalability** | < 100ms SSE Latency at 50k SSE Clients | Independent horizontal scaling of Gateways (HPA based on HTTP volume), Coordinators (Etcd Hash Ring partition assignment), and Workers (Kubernetes KEDA scaling based on NATS queue depth). |
| **P3** | **Fault Tolerance & Resiliency** | 99.999% Zero Data Corruption | Worker OS watchdogs (`syscall.Statfs`), S3 Thundering Herd Circuit Breakers ([`breaker.go`](../internal/worker/breaker.go#L20)), Atomic S3 `.tmp` commit renames, and Exponential Backoff Dead Letter Queues ([`dlq.go`](../internal/coordinator/dlq.go#L17)). |
| **P4** | **Zero-Cost Deployment** | 4 OCPUs / 24GB RAM (Free) | Native compilation for ARM64 (Oracle Cloud Ampere A1), Tailscale overlay mesh networking, aggressive memory management, and local MinIO emulation. |

---

## 1.3 Stakeholders & Operational Matrix

The system caters to three primary operational stakeholders, each with distinct expectations and integration points:

| Stakeholder Role | Primary Expectation | System Integration Interface | SLA Requirement |
| :--- | :--- | :--- | :--- |
| **End Users & App Clients** | Ultra-fast upload speeds, real-time progress bars (0% to 100%), and instant HLS/DASH playback without buffering. | SSE Endpoint (`GET /progress/{uuid}`), Presigned Batch API (`POST /api/jobs/{uuid}/urls`). | < 100ms progress stream latency; zero upload bandwidth bottlenecks. |
| **Platform & SRE Engineers** | Zero-downtime rolling updates, deterministic KEDA autoscaling, Jaeger distributed tracing, and Prometheus metrics. | Admin Telemetry API (`GET /api/admin/regions`), OpenTelemetry Collector (`:4317`), Prometheus Metrics (`:9090`). | 100% observability across all distributed trace roots (`TraceID = JobUUID`). |
| **Developers & DevOps** | 60-second local boot ("Platform-in-a-Box") without complex external cloud account setups or paid SaaS tools. | Local Compose Stack ([`docker-compose.prod.yml`](../docker-compose.prod.yml)), Cobra CLI (`cmd/transcoder/main.go`). | Boot entire stack locally using `docker compose up` in under 60 seconds. |

---

## 1.4 Scale-to-Fit Topology & Developer UI Components

Tessera is built with two operational goals in mind: allowing **flexible, self-managed capacity scaling** and offering **ready-made visual SDK components** so that any developer can add robust video capability to their app within minutes.

### 1. Flexible Scale-to-Fit Topology
The engine's architecture accommodates widely different load profiles without requiring code alterations. Developers configure the system topology according to their exact user volume:
* **Scale-Down (e.g., 50K Users)**: Run Gateway, Coordinator, and Worker pools on small, lightweight Virtual Machines (VMs) using basic CPU transcoding. You only pay for raw compute, bypassing commercial per-minute billing fees.
* **Scale-Up (e.g., 50M+ Users)**: Deploy clustered API Gateway fleets, a 1024-partition active hash ring with etcd consensus fencing, NATS task sharding, and GPU-accelerated auto-scaling workers. Manifest-only Cross-Region Replication (CRR) keeps heavy chunk payloads local to their PoP, minimizing WAN bandwidth bills.
* *For exact VM instance specifications and config mappings, refer to the [Deployment Sizing Tiers Manual](deployment_scaling_tiers.md).*

### 2. Ready-Made Ingress & Playback UI Components
Tessera provides an embedded React/TypeScript `ui-sdk` containing modular, highly customizable frontend components to build the complete user video experience:
* **Video Ingress (`VideoUploader`)**: A client-side upload component that requests presigned S3 URLs from the Gateway, uploads raw media chunks directly to object storage in parallel, and renders real-time progress indicators.
* **Adaptive Streaming (`VideoPlayer`)**: An advanced, themeable video player wrapper built around `hls.js`. It includes real-time telemetry diagnostics overlay, playback speed/quality selectors, picture-in-picture, and customizable overlay seek-skip chevrons.
* **Media Feed Tiles (`VideoTile`)**: An interface-ready video thumbnail component. Hovering over a tile triggers a muted HLS preview playback directly inside the tile bounds, featuring real-time playback progress lines and remaining countdown duration counters.

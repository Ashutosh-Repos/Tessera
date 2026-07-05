# 1. Introduction and Goals

The **Distributed VOD (Video-on-Demand) Engine** is an open-source, hyper-scalable, cloud-agnostic video transcoding platform designed to rival and exceed the capabilities of proprietary enterprise solutions such as AWS Elemental MediaConvert, Bitmovin, and GCP Transcoder API. 

Modern digital media platforms encounter severe architectural challenges when processing high-resolution user-generated video content. Raw video uploads from smartphones, professional cameras, and screen recording software are typically uncompressed or high-bitrate single files ranging from several gigabytes to tens of gigabytes. Serving these raw video files directly to consumer devices results in unacceptably high bandwidth costs, severe buffering, playback incompatibility, and poor user experience on mobile networks. 

To solve this problem, media engineers rely on **Adaptive Bitrate Streaming (ABR)** protocols—predominantly Apple HLS (HTTP Live Streaming) and MPEG-DASH (Dynamic Adaptive Streaming over HTTP). ABR requires converting a raw source video into multiple discrete resolution streams (such as 1080p Full HD, 720p HD, and 480p SD), each encoded at different target bitrates and sliced into short, keyframe-aligned transport stream segments (typically 5 seconds in duration). 

Monolithic video transcoding architectures process entire video files sequentially on single large compute instances. This approach suffers from critical flaws: processing times scale linearly with video length (a 2-hour 4K video can take hours to process), compute nodes require massive local storage arrays, and node failures mid-transcode destroy all progress, forcing a complete restart of the job.

The Distributed VOD Engine solves these fundamental architectural flaws by introducing a **Shared-Nothing, 3-Tier Distributed Compute Architecture** (Gateway, Coordinator, Worker). The engine accepts massive video uploads up to 50GB, dynamically probes and streams video headers in memory without local disk I/O, chops raw video streams into independent 5-second segments, and distributes transcoding tasks across a dynamically autoscaling fleet of worker nodes. By decoupling ingress, coordination, and execution, the engine achieves massive horizontal parallelism, sub-second progress streaming, zero-downtime fault recovery, and 100% cloud vendor independence.

---

## 1.1 Core System Capabilities & Architectural Principles

The engine is engineered around five fundamental capabilities that distinguish it from legacy transcoding platforms:

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                               Distributed VOD Engine Core                                │
├──────────────────────────┬────────────────────────────────────┬──────────────────────────┤
│ Ingestion Layer          │ Execution & Slicing Layer          │ Delivery & State Layer   │
│ • Direct S3 Presigned    │ • 64KB In-Memory Stream Slicing    │ • Redis Pipeline State   │
│ • 50GB File Handling     │ • Keyframe-Aligned Segments        │ • SSE Multiplexing       │
│ • Zero Gateway Bandwidth │ • Idempotent Bitset Skipping       │ • Epoch-Fenced Manifests │
└──────────────────────────┴────────────────────────────────────┴──────────────────────────┘
```

### 1. Stateless Gateway Presigned Ingestion
Traditional API gateways ingest incoming file uploads directly through HTTP request bodies, forwarding bytes to application servers. In a video processing ecosystem, ingesting a single 50GB file through the Gateway consumes CPU, memory, and network interfaces, creating severe bandwidth bottlenecks that starve control plane traffic. The VOD Engine eliminates this bottleneck through a **Stateless Presigned Upload Flow**. The Gateway generates cryptographically signed AWS S3 / MinIO PUT URLs for multi-part chunk uploads. The client browser streams video data directly to object storage, reducing Gateway network utilization to zero and ensuring the API gateway remains completely stateless and responsive regardless of upload volume.

### 2. Zero-Disk In-Memory Stream Slicing
Traditional chunking algorithms require downloading the entire 50GB raw video from object storage onto a coordinator node's local disk before executing FFmpeg segmentation commands. This introduces multi-minute disk I/O delays, requires massive local disk arrays on control plane nodes, and risks disk exhaustion during upload spikes. The VOD Engine implements an **In-Memory Faststart Slicing Algorithm** ([`slicer.go`](../internal/coordinator/slicer.go#L45)). The Coordinator performs an HTTP Range Request reading only the first 64KB of the raw video object to inspect MP4 binary container atoms (`moov` vs `mdat`). If the Faststart atom (`moov`) is positioned at the start of the file, the Coordinator streams the file from S3 directly into `ffmpeg -i pipe:0` in memory, chunking the video on the fly and uploading 5-second raw segment slices back to S3 without ever writing bytes to local disk.

### 3. Lock-Free Worker Idempotency
Because distributed event buses (such as NATS JetStream or AWS SQS) guarantee "at-least-once" delivery, worker nodes can occasionally receive duplicate transcoding tasks due to transient network timeouts or pod rebalances. Because video encoding is extremely CPU and GPU intensive, executing duplicate transcoding tasks wastes cloud compute resources and causes race conditions in storage. The VOD Engine enforces **Lock-Free Worker Idempotency** ([`executor.go`](../internal/worker/executor.go#L87)). Before spawning an FFmpeg process, the worker computes a deterministic mathematical index (`BitIndex()`) for the task and inspects a Redis Bitset (`job:{uuid}:progress`). If the bit is already set to `1`, the worker immediately ACKs the message and skips execution in under 1 millisecond.

### 4. Real-Time Progress Stream Multiplexing
Providing granular real-time progress updates (e.g. 0% to 100% completion) to thousands of connected end-user web applications usually requires dedicated WebSocket connections per client. In high-concurrency environments with 50,000 active viewers, opening 50,000 persistent database connections crashes database connection pools. The VOD Engine resolves this with the **Progress Multiplexer** ([`multiplexer.go`](../internal/gateway/multiplexer.go#L56)). A single background goroutine on the Gateway issues a blocking `XREAD BLOCK` call against Redis Streams for all active jobs. Incoming stream events are multiplexed and fanned out in memory to Server-Sent Events (SSE) subscriber channels. If a client network connection slows down, non-blocking channel selects (`select { case ch <- update: default: }`) intentionally drop unbuffered progress frames for that specific client, guaranteeing that a slow user cannot leak memory or block the multiplexer for the rest of the cluster.

### 5. Epoch-Fenced Manifest Compilation
Network partitions and sudden node crashes can cause temporary split-brain scenarios where a new Coordinator adopts a partition while an old, unresponsive Coordinator wakes up from a GC pause. If both Coordinators attempt to generate HLS playlists simultaneously, race conditions will corrupt the final `master.m3u8` manifest. The VOD Engine enforces **Split-Brain Epoch Fencing** ([`manifest.go`](../internal/coordinator/manifest.go#L28)). Each Coordinator maintains a monotonic `currentEpoch` incremented on every ring rebalance. Before compiling playlists, the Coordinator verifies that the stored epoch in Redis matches its current epoch (`storedEpoch <= currentEpoch`). Stale Coordinators are fenced out instantly, protecting playlist integrity.

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

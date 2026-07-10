# Tessera Core Architecture & Engineering Design

Tessera is a decoupled, highly parallel, cloud-agnostic VOD engine. It consists of three independent components collaborating over standard messaging, state, and storage backends.

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

## 1. The 3-Tier Split

### API Gateway (`cmd/transcoder/main.go server gateway`)
- **Role**: Stateless edge ingress. Handles user rate limiting, CORS configuration, session initialization, and SSE progress stream multiplexing.
- **Design Philosophy**: Generates cryptographically signed S3 presigned PUT URLs so clients upload multi-part binary chunks directly to S3. Gateway network footprint stays at zero.
- **Code**: [`internal/gateway/`](../internal/gateway/)

### Coordinator (`cmd/transcoder/main.go server coordinator`)
- **Role**: Stateful control plane brain. Coordinates hash ring membership, slices MP4 files, publishes tasks, routes dead letter queue (DLQ) messages, and compiles final HLS/DASH manifest files.
- **Code**: [`internal/coordinator/`](../internal/coordinator/)

### Worker (`cmd/transcoder/main.go server worker`)
- **Role**: Stateless compute agent. Pulls tasks from the message bus, checks local host disk space boundaries, invokes FFmpeg CLI commands in isolated OS subprocesses, and commits transcoded TS segments.
- **Code**: [`internal/worker/`](../internal/worker/)

---

## 2. Partition Routing & Consensus Hash Ring

To distribute jobs deterministically without maintaining a database bottleneck:
1. **Hash Ring Mapping**: Job IDs are mapped to a partition: `FNV-1a(JobID) % 1024` ([`hashing.go`](../internal/models/hashing.go)).
2. **Ring Consensus**: Coordinators register their unique IDs in Etcd under `/coordinators/{node_id}` with a 5s lease TTL. The ring assigns 150 virtual nodes per Coordinator across 1024 partitions ([`ring.go`](../internal/coordinator/ring.go)).
3. **Partition Adoptions**: When a Coordinator joins or leaves, virtual nodes shift. The active nodes watch `/coordinators/` changes to trigger partition adoptions/renunciations, spinning up or stopping partition managers ([`partition.go`](../internal/coordinator/partition.go)).
4. **Epoch Fencing**: Each Coordinator maintains a monotonic epoch counter. Before writing final playlists, the Coordinator validates the partition's active owner epoch in Redis ([`manifest.go`](../internal/coordinator/manifest.go)). If `storedEpoch > currentEpoch`, it indicates a newer coordinator took over the partition, and this node aborts compilation.

---

## 3. Core Processing Pipeline

### Faststart Slicing ([`slicer.go`](../internal/coordinator/slicer.go))
Instead of downloading a massive 50GB file onto the coordinator disk:
1. **Header Check**: The Coordinator reads the first 1MB of the video from S3 via `GetObject` to find the MP4 container structure (`moov` vs `mdat`).
2. **Streaming Slices**:
   - If `moov` appears before `mdat` (Faststart layout), the S3 network stream is piped directly into `ffmpeg -i pipe:0 -c copy -f segment -segment_time 5` in memory.
   - If `moov` is after `mdat` (Fragmented layout), the coordinator downloads the file once, runs `ffmpeg -movflags +faststart` locally to relocate the atom, and then slices.
3. **Chunk Upload**: 5-second raw segment `.mp4` chunks are written to a temp folder, uploaded to S3, and immediately purged from the coordinator's local disk.

### Lock-Free Worker Idempotency ([`executor.go`](../internal/worker/executor.go))
To avoid dual-processing segment tasks from at-least-once message deliveries:
1. **Fast-path**: The worker computes the task `BitIndex()` and checks Redis `TaskExists`.
2. **Slow-path**: If Redis is unreachable (circuit breaker is open), the worker falls back to an S3 `HeadObject` check on the target output segment key.
3. If either check confirms the segment exists, the worker immediately ACKs the message and skips transcoding.

### State Pipeline & Manifest Compilation ([`manifest.go`](../internal/coordinator/manifest.go))
1. **Atomic Rename**: Workers upload transcoded segments first as `.tmp` files, copying them to their final `.ts` keys to avoid half-written file reads.
2. **Completion Pipeline**: The worker executes a Redis pipeline transaction in one RTT:
   - Sets the completion task lock key.
   - Sets the completion bit in the job progress bitmap.
   - Increments the completed segment count.
   - Writes the parsed segment duration to a durations hash.
   - Publishes a progress frame to the progress stream.
3. **Consistency Barrier**: On job completion, the Coordinator waits 1 second for S3 eventual consistency, checks that the final segment exists via `HeadObject`, and generates the media playlists (HLS master/media + DASH manifest).

---

## 4. Self-Healing & Failover Guards

Tessera implements robust failover mechanics at every layer to survive node, database, and storage outages.

### 4.1 Coordinator Ring Failover & Lease Loss
- When a Coordinator instance crashes or loses its network connection, its Etcd lease expires after 5 seconds.
- The remaining Coordinator nodes receive an Etcd watch trigger and rebuild the virtual hash ring ([`ring.go`](../internal/coordinator/ring.go)).
- Partitions that belonged to the crashed node are adopted by active coordinators. Newly assigned Coordinators spin up local partition managers ([`partition.go`](../internal/coordinator/partition.go)), scan Redis status hashes for pending jobs, and resume manifest compilation loops.

### 4.2 Worker Failure & NATS JetStream Redelivery
- Active tasks are pulled from NATS JetStream by workers. NATS tracks deliveries using an explicit acknowledgement model.
- If a Worker node crashes, gets evicted, or experiences a hardware fault mid-transcode, it fails to send heartbeat calls.
- After the `AckWait` deadline (configured in NATS JetStream), the message bus automatically routes the task back to the active queue, where another Worker pulls and processes it.

### 4.3 Dead Letter Queue (DLQ) & Coordinator Backoff Retries
- If a task fails repeatedly (e.g. due to input corruption or encoder syntax faults), NATS JetStream routes it to the `transcode-tasks-dlq` after `MaxDeliver` (3) failed attempts.
- The Coordinator's DLQ Monitor ([`dlq.go`](../internal/coordinator/dlq.go)) subscribes to DLQ events:
  - If a task belongs to a partition owned by another Coordinator, it is NAKed with a 2-second delay.
  - If owned, the Coordinator increments the key `task:{jobID}:{segmentIdx}:{resolution}:retries` in Redis.
  - If `retries <= 3`, the Coordinator computes an exponential backoff delay ($t_{\text{backoff}} = 10 \times 2^{\text{retries}-1}$ seconds), schedules a local Go timer, ACKs the DLQ message, and republishes the task to NATS once the timer expires.
  - If `retries > 3`, the Coordinator transitions the job state in Redis to `FAILED`, updates client SSE subscribers, and discards the task.

### 4.4 Storage Thundering Herd Protection (Circuit Breaker)
- During Redis or S3 outages, thousands of concurrent workers repeatedly calling `HeadObject` can overwhelm storage networks.
- Workers initialize a Circuit Breaker ([`breaker.go`](../internal/worker/breaker.go)) with a 5s sliding window and a 3-failure threshold:
  - If 3 S3 or Redis errors occur within 5 seconds, the breaker trips to **OPEN**.
  - Subsequent transcoding tasks bypass S3/Redis checks and immediately sleep for an exponential backoff duration ($t_{\text{backoff}} = 100\text{ms} \times 2^{\text{fails}-1}$, capped at 5s) to allow storage recovery.
  - Once the cooldown timer expires, the breaker enters **HALF-OPEN** to test a single request. If successful, it returns to **CLOSED**.

### 4.5 Worker Resource Safeguards & OS Watchdogs
- **Disk Space Pre-Flight**: Before starting FFmpeg, the executor calls `syscall.Statfs()` to inspect `/tmp/scratch`. If free disk space is below `MinDiskFreeGB` (10GB), the task is NAKed and returned to NATS JetStream to let other workers handle it ([`executor.go`](../internal/worker/executor.go#L44)).
- **Temp File Size Guard**: A dedicated watchdog thread checks the size of the transcoding output file every `WatchdogIntervalSec` seconds. If the file exceeds `MaxTempFileSizeGB` (3GB), it kills the FFmpeg process group using `syscall.Kill(-pid, SIGKILL)` ([`executor.go`](../internal/worker/executor.go#L229)).
- **Max Duration Timeout**: If FFmpeg transcoding hangs, the watchdog thread terminates the process group after `MaxTaskDurationMin` (5 minutes).

### 4.6 Worker Graceful Draining
- Upon receiving a SIGTERM signal, the Worker Daemon stops pulling new tasks from NATS ([`daemon.go`](../internal/worker/daemon.go#L101)).
- It waits up to `GracefulDrainSec` (300 seconds) for running tasks to finish.
- If the timeout expires before tasks complete, it kills remaining FFmpeg process groups using process signaling. Unfinished tasks return to NATS for redelivery.

### 4.7 Job Garbage Collector (GC)
- The Coordinator runs a background GC loop every `GCIntervalMin` (10 minutes) ([`gc.go`](../internal/coordinator/gc.go)).
- It scans active partitions, identifies completed or failed jobs older than `GCStaleThreshHours` (24 hours), deletes Redis keys, and cleans up raw S3 media paths.

---

## 5. Algorithmic Specifications

Tessera's scale and consistency rely on deterministic mathematical rules:

### 5.1 FNV-1a Consistent Hashing
Partition assignment maps the `JobID` to a partition slot (0 to 1023) using the FNV-1a 32-bit hash algorithm:

$$P = \text{FNV-1a}_{32}(\text{JobID}) \pmod{1024}$$

### 5.2 Progress Bitmap Indexing
The bitwise offset of a segment/resolution combination inside the Redis progress bitmap is calculated deterministically:

$$\text{BitIndex} = S \times R_{\text{count}} + O_{\text{res}}$$

Where:
- $S$ is the zero-based segment index.
- $R_{\text{count}}$ is the count of target resolutions (default 3: 1080p, 720p, 480p).
- $O_{\text{res}}$ is the resolution index offset (1080p = 0, 720p = 1, 480p = 2).

### 5.3 NATS Shard Routing
Tasks are balanced across sharded NATS queues using the partition ID:

$$\text{ShardID} = P \pmod{S_{\text{nats}}}$$

Where:
- $P$ is the partition ID.
- $S_{\text{nats}}$ is the `NATSShardCount` (default 4).

### 5.4 Cross-Resolution Keyframe Alignment
To allow HLS players to switch bitrates mid-stream without buffering, keyframe boundaries must match exactly across all resolutions. Tessera forces FFmpeg keyframe placement at segment boundaries by injecting copy options and forced keyframe expressions:
```bash
-copyts -force_key_frames expr:gte(t,0) -f mpegts
```
This forces keyframes at segment boundaries across 1080p, 720p, and 480p.

---

## 6. Media Storage Directory Layout

Every media file is organized deterministically inside S3:
```
jobs/partition_{0..1023}/job_{job_id}/
  ├── raw/
  │    ├── source.mp4                  # Original upload (deleted on completion/failure)
  │    ├── chunk_000.mp4               # 5-second raw video chunks
  │    └── chunk_001.mp4
  ├── transcoded/
  │    ├── segment_000_1080p.ts        # Transcoded HLS TS segments
  │    ├── segment_000_720p.ts
  │    └── segment_000_480p.ts
  ├── thumbnails/
  │    ├── thumb_0.jpg                 # Large thumbnail options (640x360)
  │    └── thumb_1.jpg
  ├── sprite/
  │    ├── sprite.jpg                  # Hover preview sprite sheet (160x90 cells)
  │    └── sprite.vtt                  # WebVTT sprite layout timesheet
  ├── 1080p.m3u8                       # Variant resolution HLS playlist
  ├── 720p.m3u8
  ├── 480p.m3u8
  ├── master.m3u8                      # HLS master playlist
  ├── manifest.mpd                     # DASH playlist
  └── job_completed.json               # Completed state sentinel
```

---

## 7. Ephemeral Redis Key Namespaces & Hash Tag Routing

In a Redis Cluster deployment, keys are distributed across 16,384 hash slots based on a CRC16 checksum. If a multi-key pipeline or transaction operates on keys mapping to different slots, Redis returns a fatal `CROSSSLOT` error.

To guarantee cluster safety and atomic execution, Tessera formats keys using **Redis Hash Tags** (wrapping the JobID in curly braces `{...}`):

$$\text{HashSlot} = \text{CRC16}(\text{"{"} + \text{JobID} + \text{"}"}) \pmod{16384}$$

Because Redis Cluster computes slots using only the text inside the curly brackets, all keys for a specific job map to the exact same cluster node.

### Redis Keys Catalog:
- **Job Status Hash**: `job:{jobID}:status` - Stores `state` (CREATED, SLICING, TRANSCODING, COMPILING, COMPLETED, FAILED), `completed`, `total`, `owner_epoch`, `partition`, and `last_updated`.
- **Progress Bitmap**: `job:{jobID}:progress` - Tracks completed segments bit-by-bit.
- **Durations Hash**: `job:{jobID}:durations` - Maps `segmentIdx_resolution` to duration string (e.g. `"5.0034"`).
- **Manifest Cache**: `job:{jobID}:manifest` - Caches manifest binary details.
- **Progress Stream**: `progress:{jobID}` - Redis Stream emitting real-time progress updates.
- **Task Done Lock**: `task:{jobID}:{segmentIdx}:{resolution}` - Temporary task lock with a 24h TTL.
- **Rate Limit IP**: `ratelimit:ip:{ipAddress}` - Global request sliding window.
- **Rate Limit User**: `ratelimit:user:{jobID}` - JWT session token rate window.
- **Active Jobs Set**: `partition:{partitionID}:active_jobs` - Set of active jobs assigned to the partition.

---

## 8. Platform Portability & GPU Acceleration

Tessera compiles natively across diverse hardware architectures:
- **OS Process Isolation**: Code uses platform-specific build tags (`process_linux.go` vs `process_darwin.go` and `executor_linux.go` vs `executor_darwin.go`) to compile process group signaling and process limiting structures on Linux server nodes while supporting local dev runs on macOS.
- **GPU Acceleration Profile**: The Worker supports the `HWAccel` configuration flag:
  - `nvenc`: Offloads H.264 encoding to NVIDIA GPUs (`-hwaccel cuda -c:v h264_nvenc`).
  - `vaapi`: Offloads to Intel/AMD graphics cards (`-hwaccel vaapi -c:v h264_vaapi`).
  - `videotoolbox`: Offloads to macOS Apple Silicon GPUs (`-c:v h264_videotoolbox`).
  - `none`: Falls back to CPU-only software rendering (`-c:v libx264 -preset fast`).

---

## 9. Cluster Security & Cryptographic Authentication

Tessera enforces zero-trust security boundaries to protect ingress pipelines, message queues, and storage cells:

```
                  ┌────────────────────────────────────────┐
                  │          Security Frontiers            │
                  └───────────────────┬────────────────────┘
                                      │
         ┌────────────────────────────┼────────────────────────────┐
         ▼                            ▼                            ▼
┌──────────────────┐         ┌──────────────────┐         ┌──────────────────┐
│ Gateway API Edge │         │  S3 Storage Edge │         │  NATS Cluster    │
│ • HMAC-SHA256 JWT│         │ • AWS4-HMAC-SHA  │         │ • Mutual TLS1.3  │
│ • IP Rate Limit  │         │ • 15 Min Presign │         │ • Client Certs   │
└──────────────────┘         └──────────────────┘         └──────────────────┘
```

1. **HMAC-SHA256 Gateway Session JWT**:
   API access to data and completion endpoints requires a JWT token. Claims include `job_id`, `upload_id`, `bucket`, `key`, and 24h expiration window (`exp`). Tokens are validated on each request.
2. **15-Min Storage Presign Expiration**:
   Presigned PUT URLs generated by the gateway for S3/MinIO direct uploads expire strictly after 15 minutes, preventing link reuse or hijacking.
3. **Mutual TLS 1.3 (mTLS)**:
   Inter-node NATS communication uses client certificates validated against a local CA certificate.

---

## 10. Memory Management & Zero-Copy Streaming

To process 50GB files within minimal RAM ceilings, Tessera uses optimized streaming and pre-allocated buffers:
- **Zero-Copy In-Memory Pipe Streaming**: During stream-slicing, the Coordinator streams the file from S3's TCP socket directly into FFmpeg's standard input pipe (`pipe:0`). Bytes flow continuously without allocating temporary byte slices on the Go heap or writing intermediate files to local disk.
- **Pre-allocated manifest buffers**: HLS variant playlists and DASH manifests are compiled using pre-allocated byte buffers (`bytes.NewBufferString`), avoiding string concatenation GC churn.
- **Channel backpressure**: Explictly bounds channel buffers:
  - `sliceSem` (capacity 50) limits concurrent FFmpeg processes.
  - `taskCh` (capacity `ConcurrentTasks * 2`) prevents worker task overflow.
  - `SSE channels` (capacity 10) drops frames for slow clients using non-blocking channel selects.

---

## 11. Observability

### Core Metrics (Scraped at `:9090/metrics`)
- **Gateway**: `gateway_active_websockets`, `gateway_upload_bytes_total`, `gateway_rate_limit_rejections_total`.
- **Coordinator**: `coord_active_jobs`, `coord_slicing_duration_seconds`, `coord_dlq_depth`, `coord_partition_adoptions_total`.
- **Worker**: `worker_transcode_duration_seconds`, `worker_ffmpeg_crashes_total`, `worker_idempotency_hits_total`, `worker_circuit_breaker_open`.

### Trace Correlation
Tessera correlates spans using the standard W3C `traceparent` header (`00-[JobUUID]-[SpanID]-01`). The `JobUUID` is used directly as the root `TraceID`, allowing SREs to copy a failing Job ID and search Jaeger/OTEL logs directly.

---

## 12. Scale-to-Fit Engineering Design

Tessera's architecture is built around **strict resource encapsulation** and **stateless execution**, enabling the exact same codebase to serve both minimal hobbyist setups and massive multi-region enterprise environments.

### 🟢 Downscaling to a Single VM (10K Users Sandbox)
At low volume (Tier 1), the system prioritizes cost-efficiency and simplicity:
- **Shared-Nothing Footprint**: Memory allocations are highly optimized. Daemons boot with negligible memory overhead (< 50MB per daemon).
- **Co-Located Stack**: All components (Gateway, Coordinator, Worker) run on a single small virtual machine (e.g., AWS `t2.medium` or a local developer laptop).
- **Embedded Infrastructure**: MinIO, Redis, and NATS are easily co-located in containers. Consistent hashing partition space defaults to a smaller subset to reduce polling overhead, allowing developers to inspect the entire end-to-end pipeline locally in seconds.

### 🔴 Upscaling to Clustered Clouds (50M+ Users Global Scale)
At enterprise volumes (Tier 6), Tessera scales horizontally by isolating compute layers and offloading data movement:
- **Zero-Bandwidth API Gateway Ingress**: Gateways never buffer upload bytes. Clients upload directly to S3/MinIO via presigned URLs. Gateway nodes can scale dynamically based on HTTP connection depth, with memory usage remaining constant regardless of video file sizes (from 10MB to 50GB).
- **Consensus Partitioning (Etcd Ring)**: Adding Coordinator nodes automatically rebalances partition ownership across the cluster using Etcd. Active jobs are partitioned, meaning Coordinator clusters can scale dynamically to handle millions of active tasks without database locks or coordination conflicts.
- **NATS Task Sharding**: Task subjects are sharded (e.g., `transcode-tasks.shard.{0..N}`), allowing workers to subscribe selectively, load-balancing queue delivery across clustered brokers.
- **GPU-Accelerated Elastic Worker Fleet**: Workers run on hardware-accelerated nodes (using NVENC, VAAPI, or VideoToolbox drivers) and scale automatically using Kubernetes KEDA based on NATS JetStream queue depth.
- **Geo-Scale Multi-Region Isolation**: Workers transcode segments locally within their region. Heavy raw chunk payloads stay local to the regional S3/MinIO bucket. Multi-region syncing only replicates HLS master playlist descriptors (`master.m3u8`), avoiding heavy WAN transit fees and ensuring sub-second delivery to global edge CDNs.

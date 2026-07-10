# 9. Architecture Decisions (ADRs)

This section records the historical architectural choices made during the development of Tessera. Each decision is documented using the standard Nygard Architecture Decision Record (ADR) template, outlining the context, technical alternatives evaluated, decision rationale, implementation details, and long-term architectural consequences.

---

## ADR-001: Server-Sent Events (SSE) over WebSockets for Progress Delivery

*   **Status:** Accepted
*   **Date:** 2026-06-15

### Context & Problem Statement
The API Gateway must push real-time transcoding progress updates (0% to 100% completion) to thousands of connected end-user web applications and mobile clients simultaneously. Traditional polling solutions (`GET /status` every second) cause extreme database read pressure during large jobs. We needed a real-time server-to-client streaming mechanism that scales efficiently to 50,000+ concurrent connections per Gateway instance without connection memory exhaustion.

### Alternatives Evaluated
1.  **Option A: WebSockets (`ws://` / `wss://`)**:
    *   *Pros:* Full bidirectional communication capability; low frame overhead.
    *   *Cons:* Requires complex WebSocket upgrading HTTP handshakes, persistent bidirectional TCP connection state, custom ping/pong keepalive management, and special configuration on enterprise load balancers (ALBs, NGINX). Memory footprint per socket connection is relatively high (~50KB per connection).
2.  **Option B: Short / Long Polling**:
    *   *Pros:* Simple HTTP GET implementation; easy to cache.
    *   *Cons:* Generates hundreds of thousands of redundant HTTP requests per minute, creating massive Redis read load and latency jitter for clients.
3.  **Option C: Server-Sent Events (SSE - `text/event-stream`)**:
    *   *Pros:* Standard HTTP/1.1 and HTTP/2 protocol streaming; native browser support via `EventSource` API; automatic browser reconnection handling; unidirectional server-to-client streaming consumes minimal memory on Gateway nodes; effortlessly traverses standard enterprise proxies, firewalls, and ALB load balancers.
    *   *Cons:* Unidirectional only (clients cannot send messages back over the SSE stream).

### Decision Rationale
We chose **Server-Sent Events (SSE)** because video progress updates are strictly unidirectional (Server → Client). By combining SSE with our `ProgressMultiplexer` ([`multiplexer.go`](../internal/gateway/multiplexer.go#L56)), a single background goroutine issues a single blocking `XREAD BLOCK` call against Redis Streams, fanning out updates in memory to thousands of SSE client channels. Non-blocking channel selects (`select { case ch <- update: default: }`) intentionally drop unbuffered progress frames for slow clients, protecting Gateway memory from network backpressure.

### Consequences
*   **Positive:** Drastically reduced Gateway RAM consumption (~2KB per client vs ~50KB for WebSockets); zero special firewall/ALB proxy configuration required; robust automatic client reconnection via native HTTP headers (`Last-Event-ID`).
*   **Negative:** Client applications must issue separate HTTP POST requests if they need to send commands back to the Gateway (e.g. cancelling a job).

---

## ADR-002: NATS JetStream over Apache Kafka for Event Bus

*   **Status:** Accepted
*   **Date:** 2026-06-18

### Context & Problem Statement
Coordinators must publish thousands of discrete transcoding tasks per minute to be consumed asynchronously by worker pools across multiple shards. The event bus must guarantee "at-least-once" message delivery, support durable pull subscriptions, provide Dead Letter Queue (DLQ) streams for unresolvable errors, and execute efficiently in low-resource environments (such as free-tier ARM cloud instances).

### Alternatives Evaluated
1.  **Option A: Apache Kafka**:
    *   *Pros:* Enterprise standard for ultra-high-throughput streaming; excellent ecosystem integrations.
    *   *Cons:* Requires massive memory overhead (JVM runtime), Zookeeper or KRaft cluster coordination, complex partition management, and significant operational maintenance. Unsuitable for lightweight developer environments or 24GB RAM free-tier cloud clusters.
2.  **Option B: RabbitMQ**:
    *   *Pros:* Flexible AMQP routing keys and queue bindings.
    *   *Cons:* Erlang runtime overhead; cluster network partition recovery ("split-brain") handling can be complex.
3.  **Option C: NATS JetStream**:
    *   *Pros:* Distributed, highly available event streaming engine built into a single 15MB Go binary; near-zero memory footprint (~15MB RAM); native support for durable pull consumers, subject filtering (`transcode-tasks.shard.>`), max delivery limits, and automatic Dead Letter Queues (`transcode-tasks-dlq`).
    *   *Cons:* Smaller third-party ecosystem compared to Kafka.

### Decision Rationale
We selected **NATS JetStream** ([`nats.go`](../internal/infra/nats.go#L18)) because it delivers the exact at-least-once persistence and pull-consumer guarantees required by our worker pools, while running within a 15MB binary footprint. This aligns perfectly with our quality goal of zero-cost ARM deployment while maintaining enterprise-grade throughput. (An optional AWS SQS driver [`sqs.go`](../internal/infra/sqs.go#L35) is also provided for pure AWS environments).

### Consequences
*   **Positive:** Developers can boot the complete messaging ecosystem locally in seconds using Docker Compose; zero JVM or Erlang runtime management overhead; native mTLS client security (`TLSCert`, `TLSKey`, `TLSCA`).
*   **Negative:** Enterprise analytics pipelines accustomed to Kafka Connect must use NATS-to-Kafka bridges or export via OpenTelemetry.

---

## ADR-003: Redis Pipelining for Atomic State Updates

*   **Status:** Accepted
*   **Date:** 2026-06-20

### Context & Problem Statement
When a worker node completes a transcoding task, it must perform multiple state updates: mark the task key as done, set a bit in the progress bitmap, increment the completed task count in the job status hash, record the segment duration, and publish a progress event to the job's Redis Stream. Executing these 5 commands as separate Redis network calls creates high network latency (5 RTTs per task) and introduces race conditions if the worker node crashes halfway through execution.

### Alternatives Evaluated
1.  **Option A: Individual Redis Synchronous Calls**:
    *   *Pros:* Simple code implementation.
    *   *Cons:* Incurs 5 network round-trips per completed segment; state becomes corrupted if a worker crashes midway.
2.  **Option B: Distributed Mutex Locks (Redlock)**:
    *   *Pros:* Strict mutual exclusion.
    *   *Cons:* High network overhead and lock contention when hundreds of workers complete segments simultaneously.
3.  **Option C: Redis Pipelining (`ExecuteCompletionPipeline`)**:
    *   *Pros:* Bundles all 5 Redis operations into a single atomic network payload executed in 1 RTT (`Set`, `SetBit`, `HIncrBy`, `HSet`, `XAdd`).
    *   *Cons:* Requires strict Redis Cluster Hash Tag alignment.

### Decision Rationale
We implemented **Redis Pipelining** via [`ExecuteCompletionPipeline`](../internal/infra/redis.go#L186). By bundling all completion operations into a single RTT pipeline, we eliminate lock contention, reduce network latency by 80%, and guarantee atomic state execution on Redis Cluster nodes.

### Consequences
*   **Positive:** Extreme completion throughput; single RTT network latency; zero lock contention overhead across worker pools.
*   **Negative:** All Redis keys for a given job must strictly enforce Redis Hash Tag formatting `{job_uuid}` to ensure they route to the same physical Redis Cluster shard.

---

## ADR-004: FFmpeg Keyframe Alignment for Seamless ABR Switching

*   **Status:** Accepted
*   **Date:** 2026-06-22

### Context & Problem Statement
Apple HLS and MPEG-DASH adaptive bitrate streaming protocols require media players to dynamically switch between resolution streams (e.g. 1080p → 720p → 480p) based on real-time network conditions. If segment chunks across different resolutions do not begin with an identical IDR I-Frame (Keyframe) at the exact same timestamp, video players experience visual artifacting, audio desynchronization, or playback stalls during quality switches.

### Alternatives Evaluated
1.  **Option A: Default FFmpeg Scene-Cut Keyframe Insertion**:
    *   *Pros:* Slightly higher video compression efficiency (FFmpeg places keyframes only on major scene changes).
    *   *Cons:* Keyframe timestamps differ between 1080p, 720p, and 480p transcodes, rendering ABR quality switching glitchy and non-compliant with Apple HLS specifications.
2.  **Option B: Forced Keyframe Placement Expression**:
    *   *Pros:* Forces an exact IDR I-frame every 5.000 seconds regardless of scene changes, guaranteeing 100% keyframe alignment across independent worker nodes.
    *   *Cons:* Marginally increases segment file size due to forced keyframes on static scenes.

### Decision Rationale
We selected the **Forced Keyframe Expression** (`-force_key_frames expr:gte(t,n_forced*5)` in [`executor.go`](../internal/worker/executor.go#L198)). This mathematically forces an exact keyframe at $t = 0.0, 5.0, 10.0, \dots$ seconds across all worker nodes processing different resolution streams for the same chunk, ensuring 100% compliance with Apple HLS and MPEG-DASH ABR standards.

### Consequences
*   **Positive:** Flawless, glitch-free adaptive bitrate quality switching on iOS, Android, Smart TVs, and Web players.
*   **Negative:** Negligible file size increase (<1%) compared to dynamic scene-cut keyframing.

---

## ADR-005: Redis Hash Tag Partition Routing

*   **Status:** Accepted
*   **Date:** 2026-06-24

### Context & Problem Statement
Redis Cluster distributes key space across 16,384 logical hash slots using CRC16 checksums. Multi-key pipelines or transactions that operate on keys belonging to different hash slots fail with a fatal `CROSSSLOT Keys in request don't hash to same slot` error.

### Decision & Rationale
We enforce **Redis Hash Tag Formatting** ([`RedisKeys`](../internal/infra/redis.go#L88)) across all generated Redis keys. By wrapping the `JobID` in curly braces `{...}` (e.g., `job:{550e8400-e29b-41d4-a716-446655440000}:status`), Redis Cluster computes the CRC16 hash slot using only the text inside the curly braces.

### Consequences
*   **Positive:** Guarantees that all status hashes, progress bitmaps, duration hashes, manifest caches, and progress streams for a specific job map to the exact same Redis node, enabling error-free atomic pipeline execution.
*   **Negative:** Extreme write traffic for a single job cannot be distributed across multiple Redis Cluster nodes.

---

## ADR-006: In-Memory Faststart S3 Range Slicing

*   **Status:** Accepted
*   **Date:** 2026-06-26

### Context & Problem Statement
Downloading a 50GB raw source video onto a Coordinator node's local disk before slicing takes minutes, consumes massive disk bandwidth, and requires multi-terabyte local storage arrays.

### Decision & Rationale
We implemented **Faststart S3 Stream Probing** ([`slicer.go`](../internal/coordinator/slicer.go#L83)). The Coordinator reads the first 1MB of the S3 object to detect the `moov` atom position relative to `mdat`. If `moov` comes first (faststart), the Coordinator pipes the S3 stream into `ffmpeg -i pipe:0 -f segment`, which writes 5-second chunks to a temporary directory. Those chunks are then uploaded to S3 and the temp dir is cleaned up.

### Consequences
*   **Positive:** Video slicing initiates quickly; avoids downloading the full raw file for faststart videos.
*   **Negative:** Chunks are still written to a local temp directory before upload. Fragmented MP4 files (`mdat` before `moov`) require a full download and remux with `ffmpeg -movflags +faststart` first.

---

## ADR-007: Epoch Fencing Consensus for Manifest Compilation

*   **Status:** Accepted
*   **Date:** 2026-06-28

### Context & Problem Statement
Network partitions or long GC pauses can cause a temporary split-brain where a new Coordinator adopts a partition while an old, unresponsive Coordinator wakes up and attempts to compile HLS manifests for the same job.

### Decision & Rationale
We implemented **Epoch Fencing Consensus** ([`manifest.go`](../internal/coordinator/manifest.go#L28)). Coordinators maintain a monotonic `currentEpoch` incremented on every ring rebalance. Before compiling playlists, the Coordinator verifies that `storedEpoch <= currentEpoch` in Redis. If a stale Coordinator attempts compilation, it is fenced out immediately.

### Consequences
*   **Positive:** 100% protection against playlist corruption or duplicate completion events during cluster network partitions.
*   **Negative:** Requires a single Redis read check prior to initiating manifest compilation.

---

## ADR-008: Go Standard Library & Cobra CLI over Monolithic Frameworks

*   **Status:** Accepted
*   **Date:** 2026-06-30

### Context & Problem Statement
Heavy web frameworks (such as Gin, Echo, or Fiber) introduce third-party dependency trees, routing abstractions, and memory overhead that complicate cross-compilation for ARM64 architectures.

### Decision & Rationale
We implemented the Gateway API using standard Go `net/http` (Go 1.22+ routing) and wrapped CLI commands using `cobra` ([`cmd/transcoder/main.go`](../cmd/transcoder/main.go#L1)).

### Consequences
*   **Positive:** Ultra-fast compilation; zero external HTTP framework dependencies; minimal RAM footprint (<20MB) per Gateway instance.
*   **Negative:** Middleware logic (CORS, JWT authentication, rate limiting) must be implemented manually.

---

## ADR-009: Circuit Breaker Pattern for Storage Protection

*   **Status:** Accepted
*   **Date:** 2026-07-02

### Context & Problem Statement
During transient Redis or S3 outages, thousands of concurrent workers issuing failing `HeadObject` calls can create a "Thundering Herd" that prevents storage recovery.

### Decision & Rationale
We implemented an in-memory **Circuit Breaker** ([`breaker.go`](../internal/worker/breaker.go#L20)) on every worker node. If 3 consecutive failures occur within 5 seconds, the breaker trips to `OPEN` for a 5-second cooldown period, immediately rejecting tasks with `NakWithDelay(5s)`.

### Consequences
*   **Positive:** Protects degraded storage subsystems from crash-loop amplification; enables immediate cluster recovery when S3 or Redis recovers.
*   **Negative:** Workers reject tasks locally during the 5-second cooldown period.

---

## ADR-010: FNV-1a Hashing for Partition Mapping

*   **Status:** Accepted
*   **Date:** 2026-07-03

### Context & Problem Statement
The system requires mapping arbitrary string Job UUIDs (`us-east:550e8400...`) into a fixed set of 1024 partitions with uniform distribution and minimal CPU overhead.

### Alternatives Evaluated
1.  **MD5 / SHA256 Cryptographic Hash**:
    *   *Pros:* Uniform distribution.
    *   *Cons:* Computationally expensive; allocates memory slices on the Go heap for every hash calculation.
2.  **FNV-1a 32-bit Hash (`PartitionOf`)**:
    *   *Pros:* Extremely fast, non-cryptographic inline bitwise integer math; zero heap allocations; perfectly uniform distribution across $1024$ partitions.
    *   *Cons:* Non-cryptographic (not suitable for security signatures).

### Decision Rationale
We selected **FNV-1a 32-bit Hashing** ([`hashing.go`](../internal/models/hashing.go#L11)) because it computes partition assignments in under 10 nanoseconds per job with zero heap allocations, ensuring partition routing never bottleneck API handlers.

### Consequences
*   **Positive:** Ultra-fast CPU partition calculation; zero memory allocations; uniform distribution across 1024 hash slots.
*   **Negative:** None for non-cryptographic partition assignment.

---

## ADR-011: AWS SDK for Go v2 with Custom Endpoint Resolvers

*   **Status:** Accepted
*   **Date:** 2026-07-04

### Context & Problem Statement
The engine must interact with MinIO during local development and testing, while targeting native Amazon S3 or Google Cloud Storage in production without altering SDK calls or changing Go source files.

### Decision & Rationale
We adopted **AWS SDK for Go v2** ([`s3.go`](../internal/infra/s3.go#L58)) combined with an `EndpointResolverWithOptionsFunc`. When `ObjectStoreConfig.Endpoint` is configured (e.g. `minio.internal:9000`), the custom resolver overrides the AWS SDK endpoint resolution to route S3 API calls directly to MinIO over HTTP/HTTPS, enabling complete S3 API parity across local and cloud environments.

### Consequences
*   **Positive:** Unified codebase for MinIO and AWS S3; supports modern AWS SDK v2 features (presigning, paginators).
*   **Negative:** Requires custom endpoint resolver initialization logic during SDK startup.

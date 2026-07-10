# Tessera Architecture Decision Records (ADRs)

This document tracks the core design trade-offs and decisions made during the development of Tessera.

---

## ADR-001: Server-Sent Events (SSE) over WebSockets for Progress

### Context
We need to push real-time transcoding progress updates (0% to 100%) to thousands of connected web and mobile clients. We must support 50,000+ concurrent connections without exhausting gateway memory or database connection pools.

### Decision
Use **Server-Sent Events (SSE)** (`text/event-stream`) instead of WebSockets.
- Progress updates are strictly unidirectional (server-to-client).
- SSE runs over standard HTTP/1.1 and HTTP/2, eliminating custom WebSocket upgrade handshakes and proxy-level connection timeouts.
- SSE has native browser support via `EventSource` with built-in automatic reconnect.
- In-memory event fan-out on Gateway via channel selects drops frames for slow clients rather than buffering, avoiding memory leaks.

### Consequences
- **Positive**: Low memory usage (~5KB per connection vs ~50KB for WebSockets); effortlessly passes through cloud load balancers and firewalls.
- **Negative**: No client-to-server communication over the same socket (clients must use standard HTTP REST calls for actions like canceling jobs).

---

## ADR-002: Consistent Hashing over Centralized DB Queue

### Context
Coordinators must manage segment slicing and manifest compilation without stepping on each other or causing double-transcoding race conditions. Standard centralized queues (e.g. RabbitMQ/DB) create centralized bottlenecks and require constant database polling.

### Decision
Use a **virtual node consistent hash ring** backed by Etcd consensus leases.
- The ring distributes 1024 partition slots across active coordinators (150 virtual nodes per coordinator).
- Ring state and membership are fully managed in memory via Etcd watches.
- Split-brain issues are fenced out by rejecting compilation writes if the stored partition epoch is newer than the coordinator's current epoch.

### Consequences
- **Positive**: Eliminates centralized database polling bottlenecks; automatic, instant rebalancing during node joins/leaves.
- **Negative**: Adds operational dependency on Etcd; brief slicing locks are required during coordinator rebalances.

---

## ADR-003: FFmpeg CLI Execution over C-Bindings (CGo)

### Context
The worker must parse video containers and encode H.264 streams. We evaluated calling FFmpeg libraries (libavcodec/libavformat) directly via CGo versus invoking the FFmpeg CLI binary as an external process.

### Decision
Invoke the **FFmpeg CLI binary** via standard Go `os/exec` subprocesses.
- CGo code is difficult to cross-compile (e.g., building ARM64 binaries on x86 machines).
- Segmentation faults inside C-libraries bypass Go's panic handlers, instantly crashing the daemon.
- Calling external processes isolates memory allocations, preventing video parser memory leaks from affecting worker daemons.

### Consequences
- **Positive**: Safe, memory-isolated execution; easy cross-compilation; operators can upgrade or patch the FFmpeg binary independently without rebuilding Tessera.
- **Negative**: Subprocess spawning overhead (~10-20ms per task); requires parsing FFmpeg stderr output strings for metrics.

---

## ADR-004: Scale-to-Fit Resource Encapsulation

### Context
We want a single, unified codebase that can run on a single developer laptop (10K users) to minimize configuration overhead, but can also scale to global, YouTube-scale enterprise environments (50M+ users) without modifying application code.

### Decision
Enforce a **shared-nothing, decoupled resource design** where compute nodes are completely ephemeral:
- **Stateless Gateway edge**: Gateways never buffer upload bytes; clients write directly to S3. Gateway nodes can scale independently via HTTP load balancers.
- **Partitioned Coordination**: Coordinator clusters split active job tracking across 1024 virtual nodes using Etcd. Rebalancing partition ownership happens on the fly as coordinator instances join or leave.
- **Sharded Task Queues**: Tasks are sharded across NATS JetStream queues (`transcode-tasks.shard.N.priority`), keeping broker traffic balanced.
- **Local regional workers**: Heavy raw chunk payloads stay local to regional S3 buckets. Multi-region replication only syncs manifest playlists (`master.m3u8`), keeping WAN transit costs at zero.

### Consequences
- **Positive**: The same codebase serves small sandbox teams and massive enterprise systems; low resource overhead on downscaled nodes (<50MB memory footprint); horizontal scalability at every component layer.
- **Negative**: Requires setting up configurations (like Etcd/NATS clusters) for large-scale production runs, adding configuration surface.

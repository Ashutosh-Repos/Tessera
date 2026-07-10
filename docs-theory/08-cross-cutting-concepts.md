# 8. Cross-Cutting Concepts

This section details the cross-cutting architectural concepts, observability frameworks, security boundaries, memory management strategies, and data routing rules enforced across all micro-components of Tessera.

---

## 8.1 Prometheus Metrics Catalog & Alertmanager Rules

Each tier boots an independent metrics HTTP server on port `:9090` (configured via `Metrics.ListenAddr`). Prometheus scrapes metrics via standard HTTP GET calls to `/metrics` using `promhttp.Handler()` ([`metrics.go`](../internal/metrics/metrics.go#L1)).

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                                Prometheus Metrics Catalog                                │
├──────────────────────────────────────────────────────────────────────────────────────────┤
│ Gateway Tier Metrics                                                                     │
│ • `gateway_upload_requests_total`       (Counter)   - Total upload sessions initialized  │
│ • `gateway_upload_bytes_total`          (Counter)   - Total raw video volume in bytes    │
│ • `gateway_active_websockets`           (Gauge)     - Active SSE client connection count │
│ • `gateway_presigned_url_latency_ms`    (Histogram) - Presigned PUT signing latency (ms) │
│ • `gateway_rate_limit_rejections_total` (Counter)   - Rejections by IP/User limiters     │
├──────────────────────────────────────────────────────────────────────────────────────────┤
│ Coordinator Tier Metrics                                                                 │
│ • `coord_active_jobs`                   (Gauge)     - Active jobs in owned partitions    │
│ • `coord_slicing_duration_seconds`      (Histogram) - S3 probing & streaming slice time  │
│ • `coord_manifest_compilation_seconds`  (Histogram) - HLS/DASH manifest compilation time│
│ • `coord_partition_adoptions_total`     (Counter)   - Partition adoptions on ring change │
│ • `coord_dlq_depth`                     (Gauge)     - Unresolved tasks queued in DLQ     │
│ • `coord_gc_orphaned_jobs_total`        (Counter)   - Abandoned jobs purged by GC daemon │
├──────────────────────────────────────────────────────────────────────────────────────────┤
│ Worker Tier Metrics                                                                      │
│ • `worker_transcode_duration_seconds`   (Histogram) - FFmpeg chunk transcoding duration  │
│ • `worker_ffmpeg_crashes_total`         (Counter)   - Non-zero FFmpeg CLI exit crashes   │
│ • `worker_idempotency_hits_total`       (Counter)   - Duplicate tasks skipped via Bitset │
│ • `worker_circuit_breaker_open`         (Gauge)     - Circuit breaker status (1=OPEN)    │
│ • `worker_disk_free_bytes`              (Gauge)     - Free scratch disk space (bytes)    │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

### Deep Analysis of Metric Types & Operational Purpose

1. **Counters (`_total`)**: Monotonically increasing values used to calculate rates via Prometheus `rate()` functions (e.g. `rate(worker_ffmpeg_crashes_total[5m])`).
2. **Gauges**: Instantaneous point-in-time state indicators (e.g. `worker_circuit_breaker_open` $= 1$ when tripped, $0$ when healthy).
3. **Histograms (`_seconds`, `_ms`)**: Measures execution latency distributions into discrete buckets (`0.1s, 0.5s, 1s, 2s, 5s, 10s`), allowing calculation of 95th and 99th percentile SLAs via `histogram_quantile()`.

### Prometheus Alertmanager Rules Configuration (`alerts.yaml`)

```yaml
groups:
- name: VODEngineAlerts
  rules:
  - alert: WorkerFFmpegCrashSpike
    expr: rate(worker_ffmpeg_crashes_total[5m]) > 0.05
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "High FFmpeg crash rate detected on workers"
      description: "FFmpeg crashes exceed 5% of total task executions over the last 5 minutes."

  - alert: StorageCircuitBreakerTripped
    expr: worker_circuit_breaker_open == 1
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: "Worker Storage Circuit Breaker is OPEN"
      description: "Worker circuit breaker tripped due to 3 consecutive S3/Redis failures within 5 seconds."

  - alert: DLQBacklogBuilding
    expr: coord_dlq_depth > 20
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: "Transcoding Dead Letter Queue backlog building"
      description: "DLQ depth has exceeded 20 unresolvable tasks for more than 5 minutes."
```

---

## 8.2 OpenTelemetry Distributed Tracing & W3C Header Format

The engine correlates distributed logs and metrics across all micro-components by using the `JobUUID` as the root `TraceID` ([`tracing.go`](../internal/tracing/tracing.go#L15)).

```
HTTP Ingress Span (Gateway)
  ├── TraceID = JobUUID
  └── NATS Header Propagation Span (Gateway Bridge)
       └── Slicing & Task Sharding Span (Coordinator)
            ├── NATS Task Queue Payload (SegmentTask.JobID)
            └── Worker Execution Span (Worker 1..N)
                 ├── FFmpeg CLI Process Execution Span
                 └── S3 Atomic Commit (.tmp -> .ts) Span
```

### W3C Trace Context Propagation Header
NATS messages and inter-tier HTTP calls format trace context using the standard W3C `traceparent` header:

$$\text{traceparent} = \text{00-}(\text{JobUUID})-\text{(SpanID)-01}$$

Example header string: `00-550e8400e29b41d4a716446655440000-4bf92f3577b34da6-01`

- **Version (`00`)**: W3C specification version.
- **TraceID (`550e...`)**: 128-bit hexadecimal encoding of the `JobUUID`.
- **SpanID (`4bf9...`)**: 64-bit hexadecimal identifier of the current execution step.
- **TraceFlags (`01`)**: `01` indicates the trace context was sampled.

All logs and trace spans export over gRPC (`otlptracegrpc`) to `otel-collector:4317` for Jaeger / Datadog integration.

---

## 8.3 Redis Hash Tag Routing & CRC16 Slot Math

In a Redis Cluster deployment, keys are distributed across 16,384 hash slots based on the CRC16 checksum of the key string. If a multi-key pipeline or transaction operates on keys that map to different hash slots, Redis returns a fatal `CROSSSLOT Keys in request don't hash to same slot` error.

To guarantee cluster safety, the engine wraps the `JobID` in curly braces `{...}` for all Redis keys belonging to the same job:

$$\text{HashSlot} = \text{CRC16}(\text{"{"} + \text{JobID} + \text{"}"}) \pmod{16384}$$

Keys created for job `550e8400-e29b-41d4-a716-446655440000`:
- `job:{550e8400-e29b-41d4-a716-446655440000}:status`
- `job:{550e8400-e29b-41d4-a716-446655440000}:progress`
- `job:{550e8400-e29b-41d4-a716-446655440000}:durations`
- `job:{550e8400-e29b-41d4-a716-446655440000}:manifest`
- `progress:{550e8400-e29b-41d4-a716-446655440000}`

Because Redis Cluster computes the CRC16 hash slot using only the substring inside the `{...}` brackets, all keys for a specific job map to the exact same Redis cluster node. This enables atomic multi-key pipeline execution ([`ExecuteCompletionPipeline`](../internal/infra/redis.go#L186)) without risk of `CROSSSLOT` failures.

---

## 8.4 Security, Authentication & Cryptographic Verification

The engine enforces security at every tier to prevent unauthorized media access, API abuse, and cluster spoofing.

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

### 1. Gateway JWT Authentication (`UploadSessionClaims`)
API access to job management endpoints (`/api/jobs/{uuid}/urls`, `/api/jobs/{uuid}/complete`) requires a JSON Web Token issued during session initialization.
- **Signing Algorithm**: HMAC-SHA256 (`jwt.SigningMethodHS256`).
- **Claims Schema**: [`UploadSessionClaims`](../internal/models/types.go#L130) contains `job_id`, `upload_id`, `bucket`, `key`, `exp` (24-hour expiration), and `iat`.
- **Validation**: Handlers verify token signatures against `Gateway.JWTSecret` and reject tampered or expired tokens with `HTTP 401 Unauthorized`.

### 2. S3 Presigned URL Security
Clients stream binary chunks directly to S3 without passing credentials to the Gateway.
- **Signing Algorithm**: `AWS4-HMAC-SHA256` query parameters.
- **Expiration Ceiling**: Presigned PUT URLs expire strictly after 15 minutes (`15 * time.Minute`), preventing link sharing or unauthorized re-uploads ([`s3.go`](../internal/infra/s3.go#L110)).

### 3. NATS Cluster Mutual TLS (mTLS)
Cluster inter-node communication is protected against MITM (Man-in-the-Middle) attacks and unauthorized container connections.
- **TLS Version**: `tls.VersionTLS13` minimum enforce specification.
- **Certificate Verification**: All Gateways, Coordinators, and Workers present client certificates (`TLSCert`, `TLSKey`) validated against the cluster Certificate Authority (`TLSCA`) ([`nats.go`](../internal/infra/nats.go#L27)).

---

## 8.5 Memory Management, Buffer Pooling & Zero-Copy Streaming

To run smoothly within the 24GB RAM ceiling of Oracle Cloud's Always Free tier while processing 50GB video streams:

### 1. Zero-Copy In-Memory Pipe Streaming
During video slicing, the Coordinator opens an S3 HTTP reader stream and connects it directly to FFmpeg's standard input stream (`pipe:0`). Bytes flow continuously from S3's TCP socket through Go's `io.Copy` buffer into FFmpeg's stdin pipe without allocating temporary byte slices on the Go heap or writing intermediate files to local disk ([`slicer.go`](../internal/coordinator/slicer.go#L45)).

### 2. Manifest Buffer Pooling (`bytes.Buffer`)
When building HLS playlists (`.m3u8`) and DASH manifests (`.mpd`), the Coordinator utilizes pre-allocated byte buffers (`bytes.NewBufferString`) to construct string data in memory, avoiding garbage collector allocation churn from repeated string concatenations ([`manifest.go`](../internal/coordinator/manifest.go#L150)).

### 3. Channel Buffer Capacity Tuning
To prevent deadlock and limit memory growth under load, all Go channels in the system enforce explicit capacity bounds:
- **`sliceSem`**: Capacity `50` (limits max concurrent FFmpeg slicing goroutines per Coordinator).
- **`taskCh`**: Capacity `ConcurrentTasks * 2` (pre-fetches worker tasks to keep workers saturated without consuming RAM).
- **`SSE Client Channels`**: Capacity `10` (buffered for bursts; non-blocking selects drop frames if slow network connections fill the buffer).

---

## 8.6 4-Layer Defense-in-Depth Fault Recovery Matrix

The platform combines 4 independent fault-tolerance layers to ensure unhandled failures never corrupt job states or leak resources:

```
Layer 1: Worker Idempotency Check (Redis Bitset `BitIndex()`)
  └── Layer 2: In-Flight Heartbeats (NATS `msg.InProgress()`)
       └── Layer 3: Dead Letter Queue (DLQ Backoff Timer 10s/20s/40s)
            └── Layer 4: Job Garbage Collector (24h Stale Purge)
```

| Layer | Component | Failure Condition Handled | Recovery Mechanism |
| :--- | :--- | :--- | :--- |
| **Layer 1** | Worker Idempotency | Duplicate task delivery from NATS / SQS | Worker checks Redis Bitset (`BitIndex()`) before running FFmpeg. If bit == 1, skips execution and ACKs immediately. |
| **Layer 2** | In-Flight Heartbeats | Worker pod crash mid-transcode | Worker calls `msg.InProgress()` every 10s. If pod dies, heartbeats stop; JetStream `AckWait` (30s) expires and redelivers task. |
| **Layer 3** | DLQ Monitor | Corrupted raw chunk / FFmpeg syntax error | Task fails 3 times → routed to `transcode-tasks-dlq` → Coordinator DLQ Monitor applies 10s/20s/40s exponential backoff retries. |
| **Layer 4** | Job Garbage Collector | Abandoned / client-disconnected jobs | `JobGCDaemon` sweeps owned partitions every 10 min; if job age > 24h, deletes S3 `raw/` files and sets 24h Redis key TTLs. |

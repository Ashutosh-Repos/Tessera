# 6. Runtime View

This section details the runtime behavior, execution flows, cross-module interactions, algorithmic formulas, and self-healing mechanics of Tessera across normal execution and failure scenarios.

---

## 6.1 Complete Job Lifecycle (Happy Path)

The happy-path lifecycle processes a raw video file from client submission to final multi-bitrate HLS and MPEG-DASH stream delivery.

```mermaid
sequenceDiagram
    autonumber
    actor Client as End User / App
    participant GW as API Gateway
    participant Redis as Redis Cluster
    participant Etcd as Etcd Consensus
    participant S3 as Object Storage
    participant Coord as Coordinator (Partition Owner)
    participant NATS as NATS JetStream
    participant Worker as Transcode Worker

    %% Phase 1: Upload Session & Presigned Ingestion
    rect rgb(240, 248, 255)
    Note over Client, GW: Phase 1: Session Initialization & Direct Upload
    Client->>GW: POST /api/jobs/upload-session (FileSizeBytes, ContentType)
    GW->>Redis: IncrRateLimit("ratelimit:ip:{ip}", 60)
    GW->>GW: Compute PartitionID = PartitionOf(JobID, 1024) via FNV-1a
    GW->>S3: CreateMultipartUpload("jobs/partition_{P}/job_{J}/raw/source.mp4")
    S3-->>GW: UploadID
    GW->>S3: PutObject("jobs/partition_{P}/job_{J}/job_manifest.json")
    GW->>Redis: SetJobStatus(JobID, State: CREATED, Partition: P)
    GW->>Redis: AddActiveJob(PartitionID, JobID)
    GW-->>Client: UploadSession (SessionToken, UploadID, PartSize)

    Client->>GW: POST /api/jobs/{uuid}/urls (PartNumbers: 1..N, Auth: JWT)
    GW->>S3: GeneratePresignedPUT(PartNum, UploadID)
    S3-->>GW: Signed PUT URLs (15 min TTL)
    GW-->>Client: PresignedBatch URLs

    Note over Client, S3: Client uploads binary chunks directly to S3 storage
    Client->>S3: PUT Chunk 1..N (Direct Transfer)
    Client->>GW: POST /api/jobs/{uuid}/complete (Parts: [{PartNum, ETag}])
    GW->>S3: CompleteMultipartUpload(UploadID, Parts)
    GW->>NATS: PublishEvent("s3-raw-uploads.job.partition_{P}.job_{J}")
    GW-->>Client: HTTP 200 {"status":"completed"}
    end

    %% Phase 2: Slicing & Task Generation
    rect rgb(255, 250, 240)
    Note over Coord, NATS: Phase 2: Stream Slicing & Task Sharding
    NATS-->>Coord: Consume "s3-raw-uploads.job.partition_{P}.>"
    Coord->>Etcd: AcquireSlicingLock(JobID, TTL: 10s)
    Coord->>Coord: Acquire Slicing Semaphore (sliceSem)
    Coord->>S3: GetObject (read first 1MB)
    S3-->>Coord: 1MB Header Data
    Coord->>Coord: Parse MP4 Atoms (`moov` vs `mdat`)
    alt `moov` at start (Faststart)
        Coord->>S3: Open Full Stream
        Coord->>Coord: Pipe S3 stream into `ffmpeg -i pipe:0 -f segment`
    else `mdat` at start (Fragmented)
        Coord->>S3: Download full raw video to local scratch disk
        Coord->>Coord: `ffmpeg -i temp.mp4 -movflags +faststart fast.mp4`
        Coord->>Coord: `ffmpeg -i fast.mp4 -f segment`
    end
    Coord->>S3: PutObject("raw/chunk_%03d.mp4") [For each 5s slice]
    Coord->>S3: PutObject("job_manifest.json") [Updated segment_count & total_tasks]
    Coord->>Redis: SetJobStatus(State: TRANSCODING, Total: total_tasks)
    loop For each Segment (0..N-1) & Resolution (1080p, 720p, 480p)
        Coord->>NATS: PublishTaskAsync("transcode-tasks.shard.{shard}.{priority}", SegmentTask)
    end
    Coord->>Etcd: ReleaseSlicingLock(JobID)
    end

    %% Phase 3: Distributed Transcoding & Dual-Path Signal
    rect rgb(240, 255, 240)
    Note over Worker, Redis: Phase 3: Transcoding, Idempotency & Progress
    NATS-->>Worker: PullTasks("transcode-tasks.shard.{shard}.>")
    Worker->>Redis: Check Bitset job:{J}:progress at BitIndex()
    alt Bit already set (Idempotent Hit)
        Worker->>NATS: TaskAck()
    else Bit not set
        loop Every 10s while transcoding
            Worker->>NATS: InProgress() [Extends AckWait deadline]
        end
        Worker->>Worker: `ffmpeg -i chunk.mp4 -vf scale={res} -force_key_frames ...`
        Worker->>S3: PutObject("transcoded/segment_%03d_%s.ts.tmp")
        Worker->>S3: CopyObject(".tmp" -> final ".ts")
        Worker->>S3: DeleteObject(".tmp")
        
        par Fast Path: Progress Stream
            Worker->>Redis: ExecuteCompletionPipeline(SetBit, HIncrBy, XAdd progress:{J})
            Redis-->>GW: XREAD BLOCK triggers in ProgressMultiplexer
            GW-->>Client: SSE Event data: {"phase":"TRANSCODING", "completed":C, "total":T, "pct":P}
        and Reliable Path: State Machine
            Worker->>NATS: PublishEvent("s3-transcoded.job.partition_{P}.job_{J}")
            Worker->>NATS: TaskAck()
        end
    end
    end

    %% Phase 4: Manifest Compilation
    rect rgb(255, 240, 245)
    Note over Coord, Client: Phase 4: Epoch-Fenced Manifest Compilation
    NATS-->>Coord: Consume "s3-transcoded.job.partition_{P}.>"
    Coord->>Redis: BitCount("job:{J}:progress")
    alt BitCount == TotalTasks
        Coord->>Redis: GetJobStatus(JobID)
        Coord->>Coord: Epoch Fencing Check (storedEpoch <= currentEpoch)
        Coord->>Redis: SetJobStatus(State: COMPILING, owner_epoch: currentEpoch)
        Coord->>Coord: Sleep 1s (S3 Eventual Consistency Barrier)
        Coord->>S3: HeadObject("transcoded/segment_{N-1}_{res}.ts") [Consistency Double-Check]
        Coord->>Coord: Generate HLS Media Playlists (1080p.m3u8, 720p.m3u8, 480p.m3u8)
        Coord->>Coord: Generate HLS Master Playlist (master.m3u8)
        Coord->>Coord: Generate MPEG-DASH Manifest (manifest.mpd)
        Coord->>S3: PutObject("master.m3u8"), PutObject("manifest.mpd"), PutObject("job_completed.json")
        Coord->>Redis: SetJobStatus(State: COMPLETED)
        Coord->>Redis: PublishProgress(Phase: COMPLETED, HLSURL, DASHURL)
        Redis-->>GW: XREAD BLOCK triggers
        GW-->>Client: SSE Event data: {"phase":"COMPLETED", "hls_url":"...", "dash_url":"..."}
        Coord->>S3: DeletePrefix("jobs/partition_{P}/job_{J}/raw/")
        Coord->>Redis: ExpireJobKeys(JobID, 86400) [24h TTL]
        Coord->>Redis: RemoveActiveJob(PartitionID, JobID)
    end
    end
```

---

## 6.2 Detailed Module Interaction & State Transition Matrix

The job state transitions through 6 distinct phases managed atomically across Redis and Object Storage.

```
 [CREATED] ───(Slicer Probe)───► [SLICING] ───(Tasks Enqueued)───► [TRANSCODING]
     │                                                                   │
     ├──────────────────────(Fatal Slicing/S3 Error)─────────────────────┤
     │                                                                   ▼
     │                                                            [COMPILING]
     │                                                                   │
     │                                                        (Manifest Compiled)
     │                                                                   │
     ▼                                                                   ▼
 [FAILED] ◄──────────────────(Max Retries / DLQ)────────────────── [COMPLETED]
```

### State Definitions & Persistence Locations

| Job Phase | Trigger Event | Redis Key Status (`job:{uuid}:status`) | Primary Action / Responsible Module |
| :--- | :--- | :--- | :--- |
| **`CREATED`** | `POST /api/jobs/upload-session` | `state: CREATED, completed: 0, total: 0` | Gateway registers upload; client uploads binary chunks directly to S3. |
| **`SLICING`** | `s3-raw-uploads` event in NATS | `state: SLICING, owner_epoch: E` | Coordinator acquires `AcquireSlicingLock`, probes MP4 header, streams slices to S3. |
| **`TRANSCODING`** | Slicing completes & tasks published | `state: TRANSCODING, total: N*R` | Workers pull `SegmentTask` from NATS shards, run FFmpeg, upload `.ts` chunks to S3. |
| **`COMPILING`** | `BitCount == TotalTasks` | `state: COMPILING, owner_epoch: E` | Coordinator verifies epoch fence, waits 1s S3 barrier, builds `master.m3u8` & `manifest.mpd`. |
| **`COMPLETED`** | Playlists & manifests written to S3 | `state: COMPLETED` | Coordinator emits completion SSE, purges `raw/` S3 prefix, sets 24h Redis TTL. |
| **`FAILED`** | Max retries exceeded / unrecoverable error | `state: FAILED, error: "..."` | DLQ Monitor or Coordinator flags job as failed, notifies progress stream, cleans up active jobs. |

---

## 6.3 Self-Healing & Failover Runtime Workflows

### 6.3.1 Coordinator Hash Ring Rebalance & Lease Loss

When a Coordinator node crashes, loses network connectivity, or experiences an Etcd lease timeout, the system executes an automated partition rebalance.

```mermaid
sequenceDiagram
    autonumber
    participant C1 as Coordinator 1 (Failing)
    participant Etcd as Etcd Cluster
    participant Ring as HashRing Module
    participant C2 as Coordinator 2 (Healthy)
    participant Redis as Redis Cluster

    Note over C1, Etcd: Scenario A: Etcd Heartbeat Loss
    C1->>Etcd: KeepAliveOnce(LeaseID)
    Etcd--XC1: Network Timeout / Lease Expired
    Note over C1: C1 detects lease failure
    C1->>C1: selfFence()
    Note over C1: 1. Set fenced = true<br/>2. Increment currentEpoch<br/>3. Stop all PartitionManagers (cancel contexts)
    
    Note over Etcd, C2: Scenario B: Ring Rebalance Event
    Etcd->>C2: WatchCoordinators Event (EventTypeDelete, NodeID: C1)
    C2->>Ring: Rebuild(activeNodes)
    Ring-->>C2: Recomputed Partition Ownership
    Note over C2: C2 discovers it now owns Partition P (previously owned by C1)
    C2->>C2: Adopt Partition P -> NewPartitionManager(P, currentEpoch)
    C2->>NATS: Subscribe "s3-raw-uploads.job.partition_P.>"
    C2->>NATS: Subscribe "s3-transcoded.job.partition_P.>"
    C2->>Redis: GetActiveJobs(Partition P)
    loop For each active job in Partition P
        C2->>Redis: GetJobStatus(JobID)
        alt Phase == SLICING (Interrupted)
            C2->>C2: Re-trigger Faststart Slicing Workflow
        else Phase == TRANSCODING
            C2->>Redis: BitCount("job:{J}:progress")
            alt BitCount == TotalTasks
                C2->>C2: Trigger Manifest Compilation (with epoch fence check)
            end
        end
    end
```

---

### 6.3.2 Worker Failure & NATS JetStream Redelivery

Workers are completely stateless. If a worker pod crashes mid-transcode (e.g. OOM killed by Linux kernel), JetStream guarantees zero task loss.

```mermaid
sequenceDiagram
    autonumber
    participant NATS as NATS JetStream Shard Queue
    participant W1 as Worker Node 1 (Crashes)
    participant W2 as Worker Node 2 (Healthy)
    participant S3 as Object Storage
    participant Redis as Redis Cluster

    NATS->>W1: PullTasks (SegmentTask: Chunk 003, 1080p)
    W1->>W1: Start FFmpeg process
    Note over W1: Worker 1 experiences OOM Panic / Node Failure
    Note over W1, NATS: InProgress() heartbeats cease!

    Note over NATS: JetStream timer exceeds AckWait (30s)
    NATS->>NATS: Increment NumDelivered counter for SegmentTask
    NATS->>W2: PullTasks (Redeliver SegmentTask, NumDelivered: 2)
    
    W2->>Redis: Check Bitset job:{J}:progress at BitIndex()
    alt Chunk was already uploaded before W1 crashed
        Redis-->>W2: Bit == 1
        W2->>NATS: TaskAck() [Skip redundant transcoding]
    else Chunk was not finished
        Redis-->>W2: Bit == 0
        W2->>W2: Execute FFmpeg
        W2->>S3: PutObject("transcoded/segment_003_1080p.tmp")
        W2->>S3: CopyObject(".tmp" -> ".ts") [Atomic Commit]
        W2->>S3: DeleteObject(".tmp")
        W2->>Redis: ExecuteCompletionPipeline(...)
        W2->>NATS: TaskAck()
    end
```

---

### 6.3.3 Dead Letter Queue (DLQ) & Exponential Backoff Retries

If a transcoding task repeatedly fails across multiple workers (e.g. corrupt input chunk or FFmpeg syntax crash), it is routed to the DLQ to prevent blocking worker pools.

```mermaid
sequenceDiagram
    autonumber
    participant Worker as Transcode Worker
    participant NATS as NATS JetStream
    participant DLQ as NATS DLQ Stream (`transcode-tasks-dlq`)
    participant Coord as Coordinator (DLQ Monitor)
    participant Redis as Redis Cluster

    Worker->>NATS: TaskNak() (Attempt 1, 2, 3)
    Note over NATS: MaxDeliver (3) exceeded for SegmentTask
    NATS->>DLQ: Route SegmentTask to `transcode-tasks-dlq`
    
    DLQ-->>Coord: SubscribeDLQ Handler receives failed Task
    Coord->>Coord: Check Ring Owner: OwnerOf(task.PartitionID) == nodeID
    alt Not Partition Owner
        Coord->>DLQ: NakWithDelay(2s) [Pass to owning Coordinator]
    else Owning Coordinator
        Coord->>Redis: IncrRateLimit("task:{J}:{seg}:{res}:retries", 86400)
        Redis-->>Coord: retryCount
        
        alt retryCount <= 3 (Max Coordinator Retries)
            Note over Coord: Compute Backoff Delay = (1 << retryCount) * 5s<br/>(Retry 1: 10s, Retry 2: 20s, Retry 3: 40s)
            Coord->>Coord: Schedule Timer(backoffDelay)
            DLQ->>Coord: TaskAck() [Remove from DLQ]
            Note over Coord: Timer Expires
            Coord->>NATS: PublishTaskAsync("transcode-tasks.shard.{shard}.{priority}", TaskPayload)
        else retryCount > 3 (Permanent Failure)
            Coord->>Redis: SetJobStatus(JobID, State: FAILED, Error: "task exceeded max retries in DLQ")
            Coord->>Redis: PublishProgress(Phase: FAILED, Error: "...")
            Coord->>Redis: RemoveActiveJob(PartitionID, JobID)
            DLQ->>Coord: TaskAck()
        end
    end
```

---

### 6.3.4 S3 Thundering Herd & Worker Circuit Breaker Tripping

When Redis or S3 experiences transient degradation, thousands of concurrent workers issuing `HeadObject` calls can crash the storage subsystem ("Thundering Herd"). The Worker Circuit Breaker prevents this.

```
 [CLOSED] ──────(3 Failures in 5s)──────► [OPEN]
    ▲                                       │
    │                                       │
(Success)                              (5s Cooldown)
    │                                       │
    └─────────── [HALF-OPEN] ◄──────────────┘
```

```mermaid
sequenceDiagram
    autonumber
    participant Worker as Task Executor
    participant CB as Circuit Breaker (`breaker.go`)
    participant S3 as Object Storage / Redis

    loop For each task
        Worker->>CB: Allow()
        alt CB State == CLOSED
            CB-->>Worker: True
            Worker->>S3: HeadObject / GetObject
            alt Network Timeout / 503 Service Unavailable
                S3--XWorker: Error
                Worker->>CB: RecordFailure()
                Note over CB: Failure count >= 3 within 5s window
                CB->>CB: State = OPEN, Trips timer (5s)
            else Success
                S3-->>Worker: OK
                Worker->>CB: RecordSuccess()
            end
        else CB State == OPEN
            CB-->>Worker: False (Circuit Breaker Tripped)
            Note over Worker: Skip S3 calls instantly!<br/>Reject task with NakWithDelay(5s) to preserve CPU & S3 bandwidth.
        else CB State == HALF-OPEN (After 5s Cooldown)
            CB-->>Worker: True (Test Request)
            Worker->>S3: HeadObject
            alt Success
                S3-->>Worker: OK
                Worker->>CB: RecordSuccess() -> State = CLOSED
            else Failure
                S3--XWorker: Error
                Worker->>CB: RecordFailure() -> State = OPEN (Reset 5s Cooldown)
            end
        end
    end
```

---

### 6.3.5 Worker Resource Safeguards & OS Watchdogs

To prevent runaway FFmpeg processes from consuming host RAM, CPU, or local disk space, the worker enforces OS-level watchdogs.

```mermaid
sequenceDiagram
    autonumber
    participant Daemon as Worker Daemon
    participant Exec as Task Executor
    participant WD as OS Watchdog Goroutine
    participant OS as Linux Kernel / System

    Daemon->>Exec: Execute(Task)
    Exec->>OS: Check Disk Space: syscall.Statfs(scratchDir)
    alt Free Disk < MinDiskFreeGB (10GB)
        Exec-->>Daemon: Return Error: "insufficient disk space"
        Daemon->>Daemon: TaskNak()
    else Disk Space OK
        Exec->>OS: Start `ffmpeg -i chunk.mp4 ...` (cmd.Start())
        Exec->>WD: Launch Watchdog Goroutine
        
        par Transcoding Monitoring
            loop Every 1s
                WD->>OS: Inspect /tmp/scratch temp file size
                alt Temp File Size > MaxTempFileSizeGB (3GB)
                    WD->>OS: pkill -9 ffmpeg / cmd.Process.Kill()
                    WD-->>Exec: Trigger Context Timeout ("temp file size exceeded limit")
                end
            end
        and Execution Timeout
            Note over WD: Timer: MaxTaskDurationMin (5 min)
            alt FFmpeg execution > 5 minutes
                WD->>OS: Kill FFmpeg process (`SIGKILL`)
                WD-->>Exec: Trigger Timeout Error
            end
        end

        alt FFmpeg exited successfully
            Exec->>WD: Cancel Watchdog Context
            Exec-->>Daemon: Return Success
        else FFmpeg Killed by Watchdog
            Exec-->>Daemon: Return Failure Error
            Daemon->>Daemon: TaskNak()
        end
    end
```

---

### 6.3.6 Worker Graceful Shutdown & Drain Sequence

When a Worker pod receives a `SIGTERM` (e.g. KEDA scaling down pods during low traffic), it drains active work gracefully without corrupting segments.

```mermaid
sequenceDiagram
    autonumber
    participant OS as Kubernetes / System
    participant Daemon as Worker Daemon
    participant Puller as NATS Task Pullers
    participant Exec as Active Executor Goroutines
    participant NATS as NATS JetStream

    OS->>Daemon: Send SIGTERM Signal
    Daemon->>Daemon: Trigger Shutdown Hook (context cancelled)
    Daemon->>Puller: stopPullers() -> Cancel pullersCtx
    Note over Puller: Stop fetching new tasks from NATS shards immediately.
    
    Daemon->>Daemon: Start GracefulDrainSec Timer (300s / 5 min)
    
    alt All active tasks finish before 300s timeout
        Exec-->>Daemon: wg.Wait() completes successfully
        Daemon->>NATS: FlushPendingPublishes() & Close connection
        Daemon-->>OS: Exit 0 (Clean Shutdown)
    else Timeout 300s reached (Stuck FFmpeg processes)
        Daemon->>Daemon: Drain timeout reached!
        Daemon->>OS: killAllFFmpeg() -> `pkill -9 ffmpeg`
        Note over NATS: In-flight unacked tasks will exceed AckWait<br/>and be automatically redelivered to other active workers.
        Daemon-->>OS: Exit 1
    end
```

---

### 6.3.7 Distributed Garbage Collection & Stale Job Reclamation

The `JobGCDaemon` runs continuously in the background on every Coordinator node to purge orphaned S3 files and expired Redis keys for abandoned or completed jobs.

```mermaid
sequenceDiagram
    autonumber
    participant GC as JobGCDaemon (`gc.go`)
    participant Ring as HashRing Module
    participant Redis as Redis Cluster
    participant S3 as Object Storage

    loop Every GCIntervalMin (e.g. 10 minutes)
        GC->>Ring: OwnedPartitions(nodeID, 1024)
        Ring-->>GC: List of Owned Partition IDs
        
        loop For each owned Partition P
            GC->>Redis: GetActiveJobs(Partition P)
            Redis-->>GC: [job_uuid_1, job_uuid_2, ...]
            
            loop For each Job UUID
                GC->>Redis: GetJobStatus(JobID)
                Redis-->>GC: status map (state, last_updated)
                
                Note over GC: Compute Age = CurrentTime - last_updated
                alt Age > GCStaleThreshHours (24 Hours)
                    Note over GC: Stale / Orphaned Job Detected!
                    alt Phase == SLICING or Phase == TRANSCODING
                        GC->>Redis: SetJobStatus(JobID, State: FAILED, Error: "job expired by GC daemon")
                    end
                    GC->>S3: DeletePrefix("jobs/partition_{P}/job_{J}/raw/")
                    GC->>Redis: ExpireJobKeys(JobID, 86400) [Force 24h expiration on all status/progress keys]
                    GC->>Redis: RemoveActiveJob(Partition P, JobID)
                    Note over GC: Increment Prometheus Metric `coord_gc_orphaned_jobs_total`
                end
            end
        end
    end
```

---

## 6.4 Progress Stream Multiplexing & Client Delivery

The Gateway multiplexes thousands of incoming Server-Sent Events (SSE) connections through a single Redis Stream reader goroutine.

```mermaid
sequenceDiagram
    autonumber
    actor Client1 as User 1 Browser
    actor Client2 as User 2 Browser
    participant GW as API Gateway Handler
    participant Mux as ProgressMultiplexer (`multiplexer.go`)
    participant Redis as Redis Cluster

    Client1->>GW: GET /progress/{job_1} (SSE Header)
    GW->>Mux: Subscribe(job_1, ch1)
    Mux->>Mux: Add job_1 to activeSubscriptions map

    Client2->>GW: GET /progress/{job_2} (SSE Header)
    GW->>Mux: Subscribe(job_2, ch2)
    Mux->>Mux: Add job_2 to activeSubscriptions map

    loop Background Loop (Every MultiplexBatchMs: 1000ms)
        Mux->>Mux: Collect active jobIDs [job_1, job_2]
        Mux->>Redis: XREAD BLOCK(1000ms) STREAMS progress:{job_1} progress:{job_2} $
        Redis-->>Mux: Return Stream Entries for job_1 and job_2
        
        loop For each returned Stream Entry
            Mux->>Mux: Match Entry JobID to Subscriber Channels
            alt Subscriber Channel 1 Ready
                Mux->>Client1: SSE Event `data: {"phase":"TRANSCODING", "pct": 45}`
            else Subscriber Channel Slow / Full
                Note over Mux: Non-Blocking Select:<br/>`select { case ch <- update: default: // Drop Frame }`
                Mux->>Mux: Drop frame for slow client (Prevents Gateway memory leak!)
            end
        end
    end

    Client1->>GW: Disconnect / Close tab
    GW->>Mux: Unsubscribe(job_1, ch1)
    Mux->>Mux: Remove job_1 from activeSubscriptions map
```

---

## 6.5 Core Algorithmic Mathematics & Formulas

### 6.5.1 Partition Mapping (`FNV-1a` Consistent Hashing)
Given a `JobID` string (e.g. `us-east:550e8400-e29b-41d4-a716-446655440000`) and partition count $P = 1024$:

$$\text{hash} = \text{FNV-1a32}(\text{JobID})$$
$$\text{PartitionID} = \text{hash} \pmod{P}$$

Code reference: [`PartitionOf`](../internal/models/hashing.go#L11).

### 6.5.2 Progress Bitmap Indexing
Given a task's segment index $S$ and target resolution $R \in \{\text{1080p}, \text{720p}, \text{480p}\}$, where $O(R)$ is the resolution array index offset ($0$ for 1080p, $1$ for 720p, $2$ for 480p):

$$\text{BitIndex}(S, R) = S \times |\text{AllResolutions}| + O(R)$$

For segment 3 at 720p: $\text{BitIndex} = 3 \times 3 + 1 = 10$.  
Code reference: [`SegmentTask.BitIndex()`](../internal/models/types.go#L79).

### 6.5.3 NATS Shard Routing
Given partition $P$, partition count $N_{p} = 1024$, and NATS shard count $N_{s} = 4$:

$$\text{Shard} = \left\lfloor \frac{P}{N_{p} / N_{s}} \right\rfloor = \left\lfloor \frac{P}{256} \right\rfloor$$

Code reference: [`PartitionManager.compileManifest`](../internal/coordinator/slicer.go#L173).

### 6.5.4 Exponential DLQ Retry Backoff
Given coordinator retry attempt $k \in \{1, 2, 3\}$:

$$\text{BackoffDelay}(k) = 2^{k} \times 5 \text{ seconds}$$

- Attempt 1: $2^1 \times 5\text{s} = 10\text{s}$
- Attempt 2: $2^2 \times 5\text{s} = 20\text{s}$
- Attempt 3: $2^3 \times 5\text{s} = 40\text{s}$

Code reference: [`runDLQMonitor`](../internal/coordinator/dlq.go#L48).

### 6.5.5 Seamless Keyframe Alignment (HLS Switching)
To guarantee seamless ABR (Adaptive Bitrate) quality switching without visual artifacting or player stalls:

$$\text{-force\_key\_frames expr:gte}(t, n_{\text{forced}} \times 5)$$

Forces an exact H.264 IDR I-frame every $5.000$ seconds across all independent workers processing 1080p, 720p, and 480p streams for the same chunk.  
Code reference: [`buildFFmpegCmd`](../internal/worker/executor.go#L198).

---

## 6.6 End-to-End Tracing & Telemetry Correlation

The OpenTelemetry system maps the `JobUUID` directly to the `TraceID` across all distributed services.

```
 [Gateway Endpoint] ──(TraceID: JobUUID)──► [NATS JetStream Header]
                                                   │
                                                   ▼
 [Coordinator Daemon] ◄──(TraceID: JobUUID)── [Worker Executor]
```

1. **Gateway**: Extracts/generates `JobUUID`. Initializes OTLP span with `TraceID = JobUUID`.
2. **NATS Message**: `SegmentTask` payload carries `JobID`. Worker reads `JobID` and initializes task span with `TraceID = JobID`.
3. **OTLP Collector**: All logs, metrics, and trace spans export over gRPC (`otel-collector:4317`) mapped to the same single trace root in Jaeger / Datadog.  
Code reference: [`tracing.InitTracer`](../internal/tracing/tracing.go#L15).

---

## 6.7 Secondary Driver Workflows (AWS SQS & Hardware Acceleration)

### 6.7.1 SQS Message Bus Provider (`sqs.go`)
When `MessageBusProvider = "sqs"` is configured in place of NATS:
- **FIFO Queues**: Shards map to FIFO queues `transcode-tasks-shard-{id}.fifo`.
- **Deduplication & Grouping**: `MessageGroupId` is set to `partitionID`, ensuring in-order task evaluation per partition while enabling parallel processing across partitions.
- **Visibility Extension**: Calls to `msg.InProgress()` invoke AWS SQS `ChangeMessageVisibility` to prevent early redelivery during long FFmpeg transcodes.
- **DLQ Redelivery**: SQS Redrive Policy routes failed tasks to `transcode-tasks-dlq.fifo` after max receive counts.  
Code reference: [`infra.NewSQSBus`](../internal/infra/sqs.go#L35).

### 6.7.2 Hardware Acceleration GPU Matrix
Workers dynamically select FFmpeg video acceleration based on `HWAccel` settings:
- `nvenc`: NVIDIA GPU H.264 hardware encoding (`-c:v h264_nvenc`).
- `vaapi`: Linux Intel/AMD GPU acceleration (`-vaapi_device /dev/dri/renderD128 -vf format=nv12,hwupload`).
- `videotoolbox`: Apple Silicon M1/M2/M3 hardware acceleration (`-c:v h264_videotoolbox`).
- `none`: Software x264 CPU fallback (`-c:v libx264 -preset fast`).  
Code reference: [`buildFFmpegCmd`](../internal/worker/executor.go#L198).

---

## 6.8 Admin Telemetry & Worker Dynamic Load Registration

```mermaid
sequenceDiagram
    autonumber
    participant Worker as Worker Daemon
    participant Redis as Redis Cluster
    participant Admin as SRE Admin Console
    participant GW as API Gateway Handler

    loop Every 2 Seconds
        Worker->>Worker: Compute simulated CPU & GPU Load<br/>CPU = 5 + ActiveTasks*15 + Jitter<br/>GPU = ActiveTasks*20 + Jitter
        Worker->>Redis: RegisterWorker(workerID, {id, cpu, gpu, tasks}, TTL: 6s)
    end

    Admin->>GW: GET /api/admin/regions (Header: Bearer AdminKey)
    par Dependency Health Checks (2s Timeout)
        GW->>Redis: Ping()
        GW->>NATS: Ping()
        GW->>S3: Ping()
        GW->>Etcd: Ping()
    end
    GW->>Redis: GetActiveWorkers()
    Redis-->>GW: Map of active worker heartbeats
    GW-->>Admin: Returns RegionHealthJSON (Healthy status, Service matrix, Active Worker list)
```

1. **Worker Load Telemetry**: Workers heartbeat CPU/GPU load and active task count into Redis with a strict 6-second TTL (`RegisterWorker`). If a worker dies, its telemetry automatically expires from the cluster within 6 seconds.
2. **Gateway Dependency Timeout**: `/api/admin/regions` runs dependency health checks (`Ping`) wrapped in a 2-second `context.WithTimeout`. If Redis, NATS, S3, or Etcd hangs, the handler returns immediately without blocking SRE dashboard rendering.  
Code reference: [`handleListRegions`](../internal/gateway/handlers.go#L422).

---

## 6.9 Summary of Critical Failure Modes & Self-Healing Guards

| Component / Layer | Failure Scenario | System Safeguard / Self-Healing Mechanism | Primary Code Location |
| :--- | :--- | :--- | :--- |
| **Gateway** | Client connection drop / network lag | Non-blocking channel push drops progress frames for slow clients to prevent memory leaks. | [`multiplexer.go`](../internal/gateway/multiplexer.go#L70) |
| **Gateway** | Redis/NATS/S3 dependency timeout | `/api/admin/regions` health handler enforces a 2-second strict context timeout to prevent gateway API hangs. | [`handlers.go`](../internal/gateway/handlers.go#L424) |
| **Coordinator** | Network partition / Etcd lease loss | Node detects lost lease during `KeepAliveOnce`, invokes `selfFence()`, cancels all active `PartitionManager` contexts, and increments `owner_epoch`. | [`daemon.go`](../internal/coordinator/daemon.go#L136) |
| **Coordinator** | Ring rebalance split-brain | `compileManifests` enforces Epoch Fencing: aborts if `storedEpoch > currentEpoch`. | [`manifest.go`](../internal/coordinator/manifest.go#L30) |
| **Coordinator** | Multiple nodes slice same video | `AcquireSlicingLock` uses Etcd `concurrency.NewMutex` to guarantee single-coordinator slicing execution. | [`etcd.go`](../internal/infra/etcd.go#L180) |
| **Coordinator** | Slicer concurrency overload | `sliceSem` buffered channel limits max concurrent slicing processes per coordinator node. | [`daemon.go`](../internal/coordinator/daemon.go#L40) |
| **Worker** | Duplicate task delivery from NATS/SQS | Worker checks Redis Bitset (`BitIndex()`) prior to FFmpeg execution; if bit is set, immediately ACKs and skips. | [`executor.go`](../internal/worker/executor.go#L87) |
| **Worker** | S3 thundering herd / 503 errors | `CircuitBreaker` trips to `OPEN` state after 3 failures in 5s, rejecting tasks with `NakWithDelay(5s)`. | [`breaker.go`](../internal/worker/breaker.go#L45) |
| **Worker** | Runaway FFmpeg / Disk fill-up | OS Watchdog checks disk space via `syscall.Statfs` and kills FFmpeg with `SIGKILL` if temp files exceed 3GB or 5 minutes. | [`daemon.go`](../internal/worker/daemon.go#L210) |
| **Worker** | Pod SIGTERM / KEDA scale-down | Worker stops pulling new tasks, enters 300s graceful drain; unacked in-flight tasks time out in JetStream/SQS and redeliver to healthy workers. | [`daemon.go`](../internal/worker/daemon.go#L105) |
| **Worker** | Corrupted / partial segment upload | Worker uploads to `segment.ts.tmp`, then performs S3 `CopyObject` -> `segment.ts` and deletes `.tmp`. | [`executor.go`](../internal/worker/executor.go#L165) |
| **DLQ** | Unrecoverable task execution error | JetStream retries task 3 times -> routes to `transcode-tasks-dlq` -> Coordinator DLQ Monitor retries with 10s, 20s, 40s backoff -> marks job `FAILED`. | [`dlq.go`](../internal/coordinator/dlq.go#L48) |
| **GC Daemon** | Abandoned / incomplete jobs in S3 | `JobGCDaemon` sweeps owned partitions every 10 min; if job age > 24h, deletes raw S3 files and sets 24h Redis key TTLs. | [`gc.go`](../internal/coordinator/gc.go#L50) |

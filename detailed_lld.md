# Low-Level Design (LLD): Distributed Video Processing & Adaptive Streaming Engine

> **⚠️ NOTICE**: This document provides the low-level design for all Go daemons. The authoritative system-wide design is in **distributed_transcoder_design_plan.md v3.1**. In case of conflict, that document takes precedence.

---

## 1. Binary Architecture & Entrypoint

The system ships as a **single Go binary** with a `--role` flag that activates different daemon subsystems.

```go
package main

import (
    "context"
    "flag"
    "log"
    "os/signal"
    "syscall"
)

func main() {
    role := flag.String("role", "", "Daemon role: gateway|coordinator|worker")
    configPath := flag.String("config", "/etc/transcoder/config.yaml", "Path to YAML config")
    flag.Parse()

    cfg, err := LoadConfig(*configPath)
    if err != nil {
        log.Fatalf("failed to load config: %v", err)
    }

    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer cancel()

    switch *role {
    case "gateway":
        daemon := NewGatewayDaemon(cfg)
        daemon.Run(ctx)
    case "coordinator":
        daemon := NewCoordinatorDaemon(cfg)
        daemon.Run(ctx)
    case "worker":
        daemon := NewWorkerDaemon(cfg)
        daemon.Run(ctx)
    default:
        log.Fatalf("unknown role: %s", *role)
    }
}
```

### 1.1 Configuration Schema

```go
// Config is the unified configuration for all three tiers.
// Each tier reads only the sections relevant to its role.
type Config struct {
    Role       string          `yaml:"role"`          // gateway | coordinator | worker
    Region     string          `yaml:"region"`        // e.g. "us-east-1"
    NodeID     string          `yaml:"node_id"`       // unique per-node identifier

    // Infrastructure Endpoints
    Redis      RedisConfig     `yaml:"redis"`
    NATS       NATSConfig      `yaml:"nats"`
    Etcd       EtcdConfig      `yaml:"etcd"`
    ObjectStore ObjectStoreConfig `yaml:"object_store"` // MinIO / S3-compatible

    // Tier-Specific
    Gateway     GatewayConfig     `yaml:"gateway"`
    Coordinator CoordinatorConfig `yaml:"coordinator"`
    Worker      WorkerConfig      `yaml:"worker"`

    // Observability
    Metrics    MetricsConfig   `yaml:"metrics"`
    Tracing    TracingConfig   `yaml:"tracing"`
}

type RedisConfig struct {
    Addrs      []string `yaml:"addrs"`      // e.g. ["redis-0:6379","redis-1:6379","redis-2:6379"]
    Password   string   `yaml:"password"`
    MaxRetries int      `yaml:"max_retries"`
    PoolSize   int      `yaml:"pool_size"`  // per shard
}

type NATSConfig struct {
    URLs       []string `yaml:"urls"`       // e.g. ["nats://nats-0:4222"]
    TLSCert    string   `yaml:"tls_cert"`   // mTLS client cert path
    TLSKey     string   `yaml:"tls_key"`
    TLSCA      string   `yaml:"tls_ca"`
}

type EtcdConfig struct {
    Endpoints  []string `yaml:"endpoints"`
    TLSCert    string   `yaml:"tls_cert"`
    TLSKey     string   `yaml:"tls_key"`
    TLSCA      string   `yaml:"tls_ca"`
}

type ObjectStoreConfig struct {
    Endpoint   string `yaml:"endpoint"`   // e.g. "minio.internal:9000"
    Bucket     string `yaml:"bucket"`
    Region     string `yaml:"region"`
    AccessKey  string `yaml:"access_key"`
    SecretKey  string `yaml:"secret_key"`
    UseSSL     bool   `yaml:"use_ssl"`
}

type GatewayConfig struct {
    ListenAddr       string `yaml:"listen_addr"`       // ":8080"
    JWTSecret        string `yaml:"jwt_secret"`
    MaxUploadSizeGB  int    `yaml:"max_upload_size_gb"` // 50
    RateLimitPerIP   int    `yaml:"rate_limit_per_ip"`  // 100/min
    RateLimitPerUser int    `yaml:"rate_limit_per_user"` // 500/day
    MultiplexBatchMs int    `yaml:"multiplex_batch_ms"` // 1000 (XREAD BLOCK timeout)
}

type CoordinatorConfig struct {
    PartitionCount     int `yaml:"partition_count"`      // 1024
    SlicingSemaphore   int `yaml:"slicing_semaphore"`    // 50
    NATSShardCount     int `yaml:"nats_shard_count"`     // 4
    EtcdLeaseTTLSec    int `yaml:"etcd_lease_ttl_sec"`   // 5
    SlicingLockTTLSec  int `yaml:"slicing_lock_ttl_sec"` // 10
    SelfFenceThreshSec int `yaml:"self_fence_thresh_sec"` // 3
    TakeoverGraceSec   int `yaml:"takeover_grace_sec"`   // 10
    GCIntervalMin      int `yaml:"gc_interval_min"`      // 10
    GCStaleThreshHours int `yaml:"gc_stale_thresh_hours"` // 24
}

type WorkerConfig struct {
    NodeID               string `yaml:"node_id"`            // inherited from global Config.NodeID at init
    ScratchDir           string `yaml:"scratch_dir"`            // "/tmp/scratch"
    MinDiskFreeGB        int    `yaml:"min_disk_free_gb"`       // 10
    WatchdogIntervalSec  int    `yaml:"watchdog_interval_sec"`  // 10
    MaxTaskDurationMin   int    `yaml:"max_task_duration_min"`  // 5
    MaxTempFileSizeGB    int    `yaml:"max_temp_file_size_gb"`  // 3
    ConcurrentTasks      int    `yaml:"concurrent_tasks"`       // 50 (per worker node)
    GracefulDrainSec     int    `yaml:"graceful_drain_sec"`     // 300 (5 minutes)
    CircuitBreakerWindow int    `yaml:"circuit_breaker_window"` // 5 seconds
    CircuitBreakerThresh int    `yaml:"circuit_breaker_thresh"` // 3 failures
    HWAccel              string `yaml:"hw_accel"`               // "nvenc" | "vaapi" | "videotoolbox" | "none"
}
```

---

## 2. Core Data Models

### 2.1 Job Lifecycle State Machine

```
                           ┌─────────────────────┐
                           │      CREATED         │ (Gateway writes job_manifest.json)
                           └──────────┬──────────┘
                                      │ S3 ObjectCreated event
                                      ▼
                           ┌─────────────────────┐
                           │      SLICING         │ (Coordinator streams + segments)
                           └──────────┬──────────┘
                                      │ All segments uploaded
                                      ▼
                           ┌─────────────────────┐
                           │    TRANSCODING       │ (Workers process tasks in parallel)
                           └──────────┬──────────┘
                                      │ BITCOUNT == total tasks
                                      ▼
                           ┌─────────────────────┐
                           │     COMPILING        │ (Coordinator builds manifests)
                           └──────────┬──────────┘
                                      │ Manifests written to S3
                                      ▼
                           ┌─────────────────────┐
                           │     COMPLETED        │ (job_completed.json sentinel)
                           └─────────────────────┘

                         ┌─────────────────────┐
                  (any)──│       FAILED         │ (DLQ / validation / timeout)
                         └─────────────────────┘
```

### 2.2 Go Type Definitions

```go
package models

import "time"

// ──────────────── Job Lifecycle ────────────────

type JobPhase string

const (
    JobPhaseCreated     JobPhase = "CREATED"
    JobPhaseSlicing     JobPhase = "SLICING"
    JobPhaseTranscoding JobPhase = "TRANSCODING"
    JobPhaseCompiling   JobPhase = "COMPILING"
    JobPhaseCompleted   JobPhase = "COMPLETED"
    JobPhaseFailed      JobPhase = "FAILED"
)

type Resolution string

const (
    Res1080p Resolution = "1080p"
    Res720p  Resolution = "720p"
    Res480p  Resolution = "480p"
)

var AllResolutions = []Resolution{Res1080p, Res720p, Res480p}

// JobManifest is written to S3 as job_manifest.json.
// It is the contract between Gateway → Coordinator → Worker.
type JobManifest struct {
    JobID        string       `json:"job_id"`
    PartitionID  int          `json:"partition_id"`
    OwnerEpoch   int64        `json:"owner_epoch"`
    Region       string       `json:"region"`
    SourcePath   string       `json:"source_path"`      // S3 key of raw upload
    SourceSizeB  int64        `json:"source_size_bytes"`
    SourceCodec  string       `json:"source_codec"`      // e.g. "h264"
    SourceFPS    float64      `json:"source_fps"`
    DurationSec  float64      `json:"duration_sec"`
    Resolutions  []Resolution `json:"resolutions"`       // target outputs
    SegmentCount int          `json:"segment_count"`     // populated after slicing
    TotalTasks   int          `json:"total_tasks"`       // segment_count × len(resolutions)
    CreatedAt    time.Time    `json:"created_at"`
}

// JobStatus maps to Redis HASH `job:{uuid}:status`.
type JobStatus struct {
    JobID       string   `json:"job_id"       redis:"job_id"`
    Phase       JobPhase `json:"phase"        redis:"state"`
    Completed   int      `json:"completed"    redis:"completed"`
    Total       int      `json:"total"        redis:"total"`
    OwnerEpoch  int64    `json:"owner_epoch"  redis:"owner_epoch"`
    PartitionID int      `json:"partition_id" redis:"partition"`
    LastUpdated int64    `json:"last_updated" redis:"last_updated"` // unix timestamp
}

// ──────────────── Task Dispatch ────────────────

// SegmentTask is the NATS message payload published by coordinators
// and consumed by workers.
type SegmentTask struct {
    JobID       string     `json:"job_id"`
    PartitionID int        `json:"partition_id"`
    OwnerEpoch  int64      `json:"owner_epoch"`
    SegmentIdx  int        `json:"segment_idx"`
    Resolution  Resolution `json:"resolution"`
    RawChunkKey string     `json:"raw_chunk_key"` // S3 key: jobs/partition_{}/job_{}/raw/chunk_003.mp4
    OutputKey   string     `json:"output_key"`    // S3 key: jobs/partition_{}/job_{}/transcoded/segment_003_1080p.ts
    HWAccel     string     `json:"hw_accel"`      // hint: "nvenc" | "vaapi" | "videotoolbox" | "none"
    Priority    string     `json:"priority"`      // "high" | "normal" | "low"
}

// BitIndex computes the deterministic index into the progress bitmap.
// Formula: segment_index × len(AllResolutions) + resolution_offset
func (t *SegmentTask) BitIndex() int {
    offset := 0
    for i, r := range AllResolutions {
        if r == t.Resolution {
            offset = i
            break
        }
    }
    return t.SegmentIdx*len(AllResolutions) + offset
}

// ──────────────── Progress Updates ────────────────

// ProgressUpdate is the JSON payload written to Redis Streams
// (XADD progress:{uuid}) and forwarded via WebSocket.
type ProgressUpdate struct {
    Phase     JobPhase `json:"phase"`
    Completed int      `json:"completed,omitempty"`
    Total     int      `json:"total,omitempty"`
    Percent   int      `json:"pct,omitempty"`
    HLSURL    string   `json:"hls_url,omitempty"`
    DASHURL   string   `json:"dash_url,omitempty"`
    Error     string   `json:"error,omitempty"`
}

// ──────────────── Upload Session ────────────────

// UploadSession is returned by POST /api/jobs/upload-session.
type UploadSession struct {
    JobID        string `json:"job_id"`
    SessionToken string `json:"session_token"` // JWT (24h expiry)
    UploadID     string `json:"upload_id"`     // S3 multipart upload ID
    PartSize     int64  `json:"part_size"`     // 50MB
    TotalParts   int    `json:"total_parts"`
    ProgressWSS  string `json:"progress_wss"`  // wss://gateway/progress/{uuid}?token=...
}

// PresignedBatch is returned by POST /api/jobs/{uuid}/urls.
type PresignedBatch struct {
    PartNumbers []int    `json:"part_numbers"`
    URLs        []string `json:"urls"`          // presigned PUT URLs (15 min expiry)
}

// CreateSessionRequest is the client payload for POST /api/jobs/upload-session. (I-11 fix)
type CreateSessionRequest struct {
    FileSizeBytes int64  `json:"file_size_bytes"` // total video file size
    FileName      string `json:"file_name"`       // original file name (for logs/metadata)
    ContentType   string `json:"content_type"`    // e.g., "video/mp4"
}
```

---

## 3. Infrastructure Interfaces

All infrastructure dependencies are accessed through Go interfaces. This enforces the Shared-Nothing model and enables unit testing via mocks.

### 3.1 Object Store Interface

```go
package infra

import (
    "context"
    "io"
    "time"
)

// ObjectStore abstracts MinIO/S3-compatible storage.
type ObjectStore interface {
    // Uploads
    CreateMultipartUpload(ctx context.Context, key string) (uploadID string, err error)
    GeneratePresignedPUT(ctx context.Context, key, uploadID string, partNum int, expiry time.Duration) (url string, err error)
    CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error
    AbortMultipartUpload(ctx context.Context, key, uploadID string) error

    // Object Operations
    PutObject(ctx context.Context, key string, body io.Reader, size int64) error
    GetObject(ctx context.Context, key string) (io.ReadCloser, error)
    HeadObject(ctx context.Context, key string) (ObjectMeta, error)
    CopyObject(ctx context.Context, srcKey, dstKey string) error
    DeleteObject(ctx context.Context, key string) error
    DeletePrefix(ctx context.Context, prefix string) error

    // Listing
    ListObjectsPrefix(ctx context.Context, prefix string) ([]string, error)
}

type CompletedPart struct {
    PartNumber int
    ETag       string
}

type ObjectMeta struct {
    Key          string
    Size         int64
    LastModified time.Time
    Exists       bool
}
```

### 3.2 State Store Interface (Redis)

```go
package infra

import "context"

// StateStore abstracts Redis Cluster operations.
// All keys MUST include {job_uuid} as a Hash Tag to prevent CROSSSLOT errors.
type StateStore interface {
    // Job Status (HASH)
    SetJobStatus(ctx context.Context, jobID string, status map[string]interface{}) error
    GetJobStatus(ctx context.Context, jobID string) (map[string]string, error)
    IncrJobCompleted(ctx context.Context, jobID string) (int64, error)

    // Progress Bitmap
    SetBit(ctx context.Context, jobID string, bitIdx int) error
    BitCount(ctx context.Context, jobID string) (int64, error)

    // Task Idempotency
    TaskExists(ctx context.Context, jobID string, segment int, res string) (bool, error)
    SetTaskDone(ctx context.Context, jobID string, segment int, res string, ttl int) error

    // Segment Durations (HASH)
    SetSegmentDuration(ctx context.Context, jobID string, segRes string, duration string) error
    GetAllDurations(ctx context.Context, jobID string) (map[string]string, error)

    // Partition Index (SET)
    AddActiveJob(ctx context.Context, partitionID int, jobID string) error
    RemoveActiveJob(ctx context.Context, partitionID int, jobID string) error
    GetActiveJobs(ctx context.Context, partitionID int) ([]string, error)

    // Manifest Cache (STRING with TTL)
    CacheManifest(ctx context.Context, jobID string, data []byte) error
    GetCachedManifest(ctx context.Context, jobID string) ([]byte, error)

    // Progress Stream (STREAM)
    PublishProgress(ctx context.Context, jobID string, update ProgressUpdate) error
    ReadProgressStream(ctx context.Context, jobIDs []string, lastIDs []string, blockMs int) ([]StreamEntry, error)

    // Deduplication (SETNX with TTL)
    DeduplicateEvent(ctx context.Context, jobID string) (bool, error) // true = first time

    // Rate Limiting
    IncrRateLimit(ctx context.Context, key string, windowSec int) (int64, error)

    // Completion Pipeline (atomic, single RTT)
    ExecuteCompletionPipeline(ctx context.Context, p CompletionPipelineParams) error

    // Cleanup (used by GC daemon)
    DeleteKeys(ctx context.Context, keys ...string) error

    // Health
    Ping(ctx context.Context) error
}

type CompletionPipelineParams struct {
    JobID        string
    SegmentIdx   int
    Resolution   string
    BitIndex     int
    Duration     string
    UnixNow      int64
    Completed    int // current completed count for progress stream
    Total        int
}

type StreamEntry struct {
    JobID   string
    ID      string // Redis Stream entry ID
    Fields  map[string]string
}
```

### 3.3 Message Bus Interface (NATS JetStream)

```go
package infra

import "context"

// MessageBus abstracts NATS JetStream operations.
type MessageBus interface {
    // Publishing (Coordinator → Workers)
    PublishTaskAsync(ctx context.Context, shard int, priority string, payload []byte) error
    FlushPendingPublishes(ctx context.Context) error // blocks until all PublishAsync futures resolve

    // Consuming (Workers pull tasks)
    PullTasks(ctx context.Context, shard int, batchSize int) ([]TaskMessage, error)

    // Partition-scoped events
    SubscribePartitionUploads(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error
    SubscribeCompletionEvents(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error

    // DLQ
    SubscribeDLQ(ctx context.Context, handler func(msg TaskMessage)) error

    // Health
    Ping(ctx context.Context) error
}

// TaskMessage wraps a NATS JetStream message.
type TaskMessage interface {
    Data() []byte
    Ack() error
    Nak() error                  // negative ack → immediate redelivery
    InProgress() error           // extend AckWait deadline
    Metadata() TaskMessageMeta
}

type TaskMessageMeta struct {
    NumDelivered int
    Timestamp    int64
}
```

### 3.4 Coordination Interface (etcd)

```go
package infra

import "context"

// Coordination abstracts etcd operations for coordinator registration and locking.
type Coordination interface {
    // Registration
    Register(ctx context.Context, nodeID string, leaseTTLSec int) (leaseID int64, err error)
    Deregister(ctx context.Context, nodeID string) error
    WatchCoordinators(ctx context.Context) (<-chan CoordinatorEvent, error)

    // Slicing Locks
    AcquireSlicingLock(ctx context.Context, jobID string, ownerID string, ttlSec int) (bool, error)
    ReleaseSlicingLock(ctx context.Context, jobID string) error
    KeepAliveLock(ctx context.Context, leaseID int64) error

    // Health
    Ping(ctx context.Context) error
}

type CoordinatorEvent struct {
    Type   EventType // PUT or DELETE
    NodeID string
    Host   string
}

type EventType int
const (
    EventTypePut    EventType = iota
    EventTypeDelete
)
```

---

## 4. Gateway Daemon Architecture

The Gateway is entirely stateless. It does not hold job progress in local memory.

### 4.1 Internal Goroutine Model

```
Gateway Process
├── HTTP Server (main goroutine)
│   ├── POST /api/jobs/upload-session   → createUploadSession()
│   ├── POST /api/jobs/{uuid}/urls      → generatePresignedBatch()
│   ├── GET  /api/jobs/{uuid}/status    → getJobStatus()         // polling fallback
│   ├── GET  /health                    → healthCheck()
│   └── WS   /progress/{uuid}          → handleWebSocket()
│
├── Progress Multiplexer (1 background goroutine)
│   └── Single XREAD BLOCK loop for ALL active WebSockets
│       → Dispatches updates to per-connection Go channels
│
├── Metrics Server (:9090 /metrics)
│
└── Graceful Shutdown Handler (SIGTERM)
```

### 4.2 WebSocket Progress Multiplexer

This is the critical component that prevents the Gateway from opening N Redis connections for N active WebSockets (which would exhaust Redis at scale).

```go
package gateway

import (
    "context"
    "sync"
)

// ProgressMultiplexer manages a single Redis XREAD BLOCK loop that fans out
// progress updates to all active WebSocket connections.
type ProgressMultiplexer struct {
    mu          sync.RWMutex
    subscribers map[string][]chan<- ProgressUpdate // jobID → list of WebSocket channels
    state       StateStore
    blockMs     int // XREAD BLOCK timeout (e.g. 1000ms)
}

func NewProgressMultiplexer(state StateStore, blockMs int) *ProgressMultiplexer {
    return &ProgressMultiplexer{
        subscribers: make(map[string][]chan<- ProgressUpdate),
        state:       state,
        blockMs:     blockMs,
    }
}

// Subscribe adds a WebSocket channel to receive updates for a specific job.
func (pm *ProgressMultiplexer) Subscribe(jobID string, ch chan<- ProgressUpdate) {
    pm.mu.Lock()
    defer pm.mu.Unlock()
    pm.subscribers[jobID] = append(pm.subscribers[jobID], ch)
}

// Unsubscribe removes a WebSocket channel.
func (pm *ProgressMultiplexer) Unsubscribe(jobID string, ch chan<- ProgressUpdate) {
    pm.mu.Lock()
    defer pm.mu.Unlock()
    subs := pm.subscribers[jobID]
    for i, s := range subs {
        if s == ch {
            pm.subscribers[jobID] = append(subs[:i], subs[i+1:]...)
            break
        }
    }
    if len(pm.subscribers[jobID]) == 0 {
        delete(pm.subscribers, jobID)
    }
}

// Run is the single background goroutine that fans out Redis Stream
// updates to all subscribed WebSockets. It reduces Redis connections
// from 50,000 (one per WebSocket) to 1 per Gateway node.
func (pm *ProgressMultiplexer) Run(ctx context.Context) {
    // Track the last-seen Stream ID per job for XREAD resume
    lastIDs := make(map[string]string) // jobID → last stream entry ID

    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        pm.mu.RLock()
        if len(pm.subscribers) == 0 {
            pm.mu.RUnlock()
            // B-8 fix: sleep to avoid busy-loop when no WebSockets are active
            time.Sleep(100 * time.Millisecond)
            continue
        }

        // Build the list of stream keys and last IDs for XREAD
        jobIDs := make([]string, 0, len(pm.subscribers))
        streamLastIDs := make([]string, 0, len(pm.subscribers))
        for jobID := range pm.subscribers {
            jobIDs = append(jobIDs, jobID)
            id, ok := lastIDs[jobID]
            if !ok {
                id = "0" // read from beginning on first subscribe
            }
            streamLastIDs = append(streamLastIDs, id)
        }
        pm.mu.RUnlock()

        // Single multiplexed XREAD BLOCK call for ALL active jobs
        entries, err := pm.state.ReadProgressStream(ctx, jobIDs, streamLastIDs, pm.blockMs)
        if err != nil {
            continue // XREAD timeout or transient error
        }

        // Fan out each entry to its subscribers
        pm.mu.RLock()
        for _, entry := range entries {
            lastIDs[entry.JobID] = entry.ID
            subs, ok := pm.subscribers[entry.JobID]
            if !ok {
                continue
            }
            update := parseProgressUpdate(entry.Fields)
            for _, ch := range subs {
                select {
                case ch <- update:
                default:
                    // Drop if client is slow — they will get a snapshot on reconnect
                }
            }
        }
        pm.mu.RUnlock()
    }
}
```

### 4.3 JWT Session & Presigned URL Flow

```go
package gateway

import (
    "context"
    "time"
    "github.com/golang-jwt/jwt/v5"
)

type UploadSessionClaims struct {
    jwt.RegisteredClaims
    JobID    string `json:"job_id"`
    UploadID string `json:"upload_id"`  // S3 multipart upload ID
    Bucket   string `json:"bucket"`
    Key      string `json:"key"`        // S3 object key
}

func (g *GatewayDaemon) CreateUploadSession(ctx context.Context, req CreateSessionRequest) (*UploadSession, error) {
    jobID := generateUUID()
    partitionID := models.PartitionOf(jobID, g.cfg.Coordinator.PartitionCount)

    // 1. Create S3 multipart upload
    s3Key := fmt.Sprintf("jobs/partition_%d/job_%s/raw/source.mp4", partitionID, jobID)
    uploadID, err := g.objectStore.CreateMultipartUpload(ctx, s3Key)
    if err != nil {
        return nil, fmt.Errorf("failed to create multipart upload: %w", err)
    }

    // 2. Write job manifest to S3
    manifest := &JobManifest{
        JobID:       jobID,
        PartitionID: partitionID,
        Region:      g.cfg.Region,
        SourcePath:  s3Key,
        SourceSizeB: req.FileSizeBytes,
        Resolutions: AllResolutions,
        CreatedAt:   time.Now(),
    }
    if err := g.writeManifestToS3(ctx, manifest); err != nil {
        return nil, err
    }

    // 3. Initialize Redis state
    g.state.SetJobStatus(ctx, jobID, map[string]interface{}{
        "state":        string(JobPhaseCreated),
        "completed":    0,
        "total":        0, // populated after slicing
        "partition":    partitionID,
        "last_updated": time.Now().Unix(),
    })
    g.state.AddActiveJob(ctx, partitionID, jobID)

    // 4. Issue long-lived JWT (24h) for JIT URL fetching
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, UploadSessionClaims{
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
            Subject:   jobID,
        },
        JobID:    jobID,
        UploadID: uploadID,
        Bucket:   g.cfg.ObjectStore.Bucket,
        Key:      s3Key,
    })
    tokenStr, _ := token.SignedString([]byte(g.cfg.Gateway.JWTSecret))

    totalParts := int(req.FileSizeBytes/(50*1024*1024)) + 1

    return &UploadSession{
        JobID:        jobID,
        SessionToken: tokenStr,
        UploadID:     uploadID,
        PartSize:     50 * 1024 * 1024, // 50MB
        TotalParts:   totalParts,
        ProgressWSS:  fmt.Sprintf("wss://%s/progress/%s?token=%s", g.cfg.Gateway.ListenAddr, jobID, tokenStr),
    }, nil
}

// GeneratePresignedBatch is called by the client with their JWT to get
// small batches of presigned PUT URLs just-in-time.
func (g *GatewayDaemon) GeneratePresignedBatch(ctx context.Context, claims UploadSessionClaims, startPart, count int) (*PresignedBatch, error) {
    batch := &PresignedBatch{}
    for i := startPart; i < startPart+count; i++ {
        url, err := g.objectStore.GeneratePresignedPUT(ctx, claims.Key, claims.UploadID, i, 15*time.Minute)
        if err != nil {
            return nil, err
        }
        batch.PartNumbers = append(batch.PartNumbers, i)
        batch.URLs = append(batch.URLs, url)
    }
    return batch, nil
}
```

### 4.4 Gateway Entry Point (D-10 Fix)

```go
// GatewayDaemon is the main gateway process, started by main() with --role=gateway.
type GatewayDaemon struct {
    cfg         Config
    state       StateStore
    objectStore ObjectStore
    multiplexer *ProgressMultiplexer
}

// Run is the gateway's main entry point. It wires the HTTP server,
// progress multiplexer, rate limiter, and metrics server. (D-10 fix)
func (g *GatewayDaemon) Run(ctx context.Context) {
    // ──── 1. Initialize Rate Limiter ────
    rl := NewRateLimiter(g.state, g.cfg.Gateway.RateLimitPerIP, g.cfg.Gateway.RateLimitPerUser)

    // ──── 2. Start Progress Multiplexer ────
    g.multiplexer = NewProgressMultiplexer(g.state, g.cfg.Gateway.MultiplexBatchMs)
    go g.multiplexer.Run(ctx)

    // ──── 3. Build HTTP Routes ────
    router := http.NewServeMux()
    router.HandleFunc("POST /api/jobs/upload-session", g.handleCreateSession)
    router.HandleFunc("POST /api/jobs/{uuid}/urls", g.handlePresignedBatch)
    router.HandleFunc("GET /api/jobs/{uuid}/uploaded-parts", g.handleListUploadedParts) // I-14: Tus resume support
    router.HandleFunc("GET /api/jobs/{uuid}/status", g.handleGetStatus)
    router.HandleFunc("GET /progress/{uuid}", g.handleWebSocketOrSSE) // I-14: Supports both WS and SSE
    router.HandleFunc("GET /health", g.handleHealth)

    // Apply rate limiting middleware
    handler := rl.Middleware(router)

    // ──── 4. Start HTTP Server ────
    srv := &http.Server{
        Addr:         g.cfg.Gateway.ListenAddr,
        Handler:      handler,
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 60 * time.Second, // long for WebSocket upgrade
        IdleTimeout:  120 * time.Second,
    }

    go func() {
        <-ctx.Done()
        // Graceful shutdown: let in-flight requests complete (5s)
        shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        srv.Shutdown(shutCtx)
    }()

    // ──── 5. Start Metrics Server ────
    go func() {
        mux := http.NewServeMux()
        mux.Handle(g.cfg.Metrics.Path, promhttp.Handler())
        metricsSrv := &http.Server{Addr: g.cfg.Metrics.ListenAddr, Handler: mux}
        go func() {
            <-ctx.Done()
            metricsSrv.Shutdown(context.Background())
        }()
        metricsSrv.ListenAndServe()
    }()

    // ──── 6. Block on HTTP Server ────
    log.Infof("gateway listening on %s", g.cfg.Gateway.ListenAddr)
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatalf("gateway HTTP server failed: %v", err)
    }
}

// handleHealth returns 200 OK for load balancer probes.
// Returns 503 during graceful drain (SIGTERM received).
func (g *GatewayDaemon) handleHealth(w http.ResponseWriter, r *http.Request) {
    // Health check includes Redis and ObjectStore ping
    if err := g.state.Ping(r.Context()); err != nil {
        http.Error(w, "redis unhealthy", http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}
```

---

## 5. Coordinator Daemon Architecture

### 5.1 Internal Goroutine Model

```
Coordinator Process
├── etcd Registration & Self-Fencing (1 goroutine)
│   ├── Registers /registry/coordinators/{node_id}
│   ├── Renews lease every leaseTTL/3 (~1.6s)
│   └── SELF-FENCE: if 2 consecutive renewals fail → cancel all consumers
│
├── Ring Watcher (1 goroutine)
│   ├── Watches etcd /registry/coordinators/ for PUT/DELETE events
│   ├── Recalculates Consistent Hash Ring on every change
│   └── Triggers partition adoption/release with takeover grace period
│
├── Partition Managers (1 goroutine per owned partition)
│   ├── NATS consumer for job-uploads.partition.{id}
│   ├── NATS consumer for task-updates.partition.{id}
│   └── Handles: slicing → dispatch → completion tracking → manifest compilation
│
├── Slicing Semaphore (shared, capacity=50)
│   └── Limits concurrent ffmpeg -c copy processes across all partitions
│
├── DLQ Monitor (1 goroutine)
│   └── Subscribes to transcode-tasks-dlq, marks failed jobs
│
├── Job GC Daemon (1 goroutine, runs every 10 min)
│   └── Scans owned partitions for stale/abandoned jobs
│
├── Metrics Server (:9090 /metrics)
│
└── Graceful Shutdown Handler (SIGTERM)
```

### 5.2 Consistent Hash Ring

```go
package coordinator

import (
    "hash/fnv"
    "sort"
    "sync"
)

const virtualNodesPerCoordinator = 150 // for balanced distribution

type HashRing struct {
    mu       sync.RWMutex
    ring     []uint32           // sorted list of virtual node hashes
    nodeMap  map[uint32]string  // hash → coordinator node ID
    members  []string           // active coordinator IDs
}

func NewHashRing() *HashRing {
    return &HashRing{
        nodeMap: make(map[uint32]string),
    }
}

// Rebuild recalculates the ring from the current set of active coordinators.
// Called on every etcd watch event (coordinator join/leave).
func (hr *HashRing) Rebuild(activeNodes []string) {
    hr.mu.Lock()
    defer hr.mu.Unlock()

    hr.ring = hr.ring[:0]
    hr.nodeMap = make(map[uint32]string)
    hr.members = activeNodes

    for _, nodeID := range activeNodes {
        for i := 0; i < virtualNodesPerCoordinator; i++ {
            key := fmt.Sprintf("%s#%d", nodeID, i)
            h := fnv.New32a()
            h.Write([]byte(key))
            hash := h.Sum32()
            hr.ring = append(hr.ring, hash)
            hr.nodeMap[hash] = nodeID
        }
    }
    sort.Slice(hr.ring, func(i, j int) bool { return hr.ring[i] < hr.ring[j] })
}

// OwnerOf returns the coordinator node ID that owns a given partition.
func (hr *HashRing) OwnerOf(partitionID int) string {
    hr.mu.RLock()
    defer hr.mu.RUnlock()

    if len(hr.ring) == 0 {
        return ""
    }

    key := fmt.Sprintf("partition:%d", partitionID)
    h := fnv.New32a()
    h.Write([]byte(key))
    hash := h.Sum32()

    // Binary search for the first virtual node >= hash
    idx := sort.Search(len(hr.ring), func(i int) bool { return hr.ring[i] >= hash })
    if idx == len(hr.ring) {
        idx = 0 // wrap around
    }
    return hr.nodeMap[hr.ring[idx]]
}

// OwnedPartitions returns all partition IDs owned by a specific node.
func (hr *HashRing) OwnedPartitions(nodeID string, totalPartitions int) []int {
    var owned []int
    for p := 0; p < totalPartitions; p++ {
        if hr.OwnerOf(p) == nodeID {
            owned = append(owned, p)
        }
    }
    return owned
}
```

### 5.3 Self-Fencing & Partition Lifecycle

```go
package coordinator

import (
    "context"
    "sync"
    "time"
)

type CoordinatorDaemon struct {
    cfg          Config
    nodeID       string
    ring         *HashRing
    state        StateStore
    bus          MessageBus
    coord        Coordination
    objStore     ObjectStore
    sliceSem     chan struct{} // buffered channel of size cfg.SlicingSemaphore
    currentEpoch int64         // monotonic epoch counter, incremented on each registration

    mu          sync.Mutex
    partitions  map[int]*PartitionManager // active partition managers
    fenced      bool                      // true if self-fenced
}

// Run is the coordinator's main entry point. It wires together all goroutines
// and blocks until ctx is cancelled (SIGTERM). (I-10 fix)
func (c *CoordinatorDaemon) Run(ctx context.Context) {
    // ──── Startup Validation ────
    // D-7 fix: ensure partition count is evenly divisible by shard count
    if c.cfg.Coordinator.PartitionCount%c.cfg.Coordinator.NATSShardCount != 0 {
        log.Fatalf("partition_count (%d) must be divisible by nats_shard_count (%d)",
            c.cfg.Coordinator.PartitionCount, c.cfg.Coordinator.NATSShardCount)
    }

    // ──── Initialize State ────
    c.sliceSem = make(chan struct{}, c.cfg.Coordinator.SlicingSemaphore)
    c.partitions = make(map[int]*PartitionManager)
    c.ring = NewHashRing()

    // ──── 1. etcd Registration + Self-Fencing ────
    go c.runEtcdRegistration(ctx)

    // ──── 2. Ring Watcher (etcd watch → hash ring rebuild → partition adoption) ────
    events, err := c.coord.WatchCoordinators(ctx)
    if err != nil {
        log.Fatalf("failed to watch coordinators: %v", err)
    }
    go func() {
        activeNodes := make(map[string]string) // nodeID → host
        for {
            select {
            case <-ctx.Done():
                return
            case evt := <-events:
                switch evt.Type {
                case EventTypePut:
                    activeNodes[evt.NodeID] = evt.Host
                case EventTypeDelete:
                    delete(activeNodes, evt.NodeID)
                }
                nodeIDs := make([]string, 0, len(activeNodes))
                for nid := range activeNodes {
                    nodeIDs = append(nodeIDs, nid)
                }
                c.onRingChange(nodeIDs)
            }
        }
    }()

    // ──── 3. DLQ Monitor ────
    go c.runDLQMonitor(ctx)

    // ──── 4. Job GC Daemon ────
    gc := &JobGCDaemon{
        coord:          c,
        intervalMin:    c.cfg.Coordinator.GCIntervalMin,
        staleThreshSec: int64(c.cfg.Coordinator.GCStaleThreshHours * 3600),
    }
    go gc.Run(ctx)

    // ──── 5. Metrics Server ────
    go func() {
        mux := http.NewServeMux()
        mux.Handle(c.cfg.Metrics.Path, promhttp.Handler())
        srv := &http.Server{Addr: c.cfg.Metrics.ListenAddr, Handler: mux}
        go func() {
            <-ctx.Done()
            srv.Shutdown(context.Background())
        }()
        srv.ListenAndServe()
    }()

    // ──── 6. Block until shutdown ────
    <-ctx.Done()

    // ──── 7. Graceful drain: release all partitions ────
    log.Info("coordinator shutting down: releasing partitions")
    c.selfFence()
    c.coord.Deregister(context.Background(), c.nodeID)
}

// runEtcdRegistration handles lease creation, keep-alive, and self-fencing.
func (c *CoordinatorDaemon) runEtcdRegistration(ctx context.Context) {
    leaseID, err := c.coord.Register(ctx, c.nodeID, c.cfg.Coordinator.EtcdLeaseTTLSec)
    if err != nil {
        log.Fatalf("failed to register in etcd: %v", err)
    }

    consecutiveFailures := 0
    ticker := time.NewTicker(time.Duration(c.cfg.Coordinator.EtcdLeaseTTLSec) * time.Second / 3)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            // D-9 fix: deregistration handled by Run() — just exit goroutine
            return
        case <-ticker.C:
            err := c.coord.KeepAliveLock(ctx, leaseID)
            if err != nil {
                consecutiveFailures++
                if consecutiveFailures >= 2 {
                    // SELF-FENCE at T=3s (after 2 failures at 1.6s intervals)
                    log.Warn("self-fencing: etcd lease renewal failed")
                    c.selfFence()
                    return
                }
            } else {
                consecutiveFailures = 0
            }
        }
    }
}

// selfFence immediately stops all NATS consumers. No state flush is needed
// because progress lives in Redis, not local RAM.
func (c *CoordinatorDaemon) selfFence() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.fenced = true
    for _, pm := range c.partitions {
        pm.Stop()
    }
    c.partitions = make(map[int]*PartitionManager)
}

// onRingChange is called when the etcd watcher detects a coordinator join/leave.
func (c *CoordinatorDaemon) onRingChange(activeNodes []string) {
    c.ring.Rebuild(activeNodes)
    newOwned := c.ring.OwnedPartitions(c.nodeID, c.cfg.Coordinator.PartitionCount)
    ownedSet := make(map[int]bool)
    for _, p := range newOwned {
        ownedSet[p] = true
    }

    c.mu.Lock()
    defer c.mu.Unlock()

    // Release partitions we no longer own
    for pid, pm := range c.partitions {
        if !ownedSet[pid] {
            pm.Stop()
            delete(c.partitions, pid)
        }
    }

    // Adopt new partitions (after takeover grace period)
    for _, pid := range newOwned {
        if _, exists := c.partitions[pid]; !exists {
            go func(partitionID int) {
                // Takeover grace period to prevent flapping
                time.Sleep(time.Duration(c.cfg.Coordinator.TakeoverGraceSec) * time.Second)
                // Re-check ownership after grace period
                if c.ring.OwnerOf(partitionID) != c.nodeID {
                    return // ownership changed during grace period
                }
                pm := NewPartitionManager(partitionID, c)
                c.mu.Lock()
                c.partitions[partitionID] = pm
                c.mu.Unlock()
                pm.Start(context.Background())
            }(pid)
        }
    }
}
```

### 5.4 Partition Manager (Slicing → Dispatch → Completion → Manifest)

```go
package coordinator

import "context"

// PartitionManager handles the full lifecycle of jobs assigned to a single partition.
type PartitionManager struct {
    partitionID int
    coord       *CoordinatorDaemon
    cancelFn    context.CancelFunc
}

func (pm *PartitionManager) Start(ctx context.Context) {
    pmCtx, cancel := context.WithCancel(ctx)
    pm.cancelFn = cancel

    // 1. Reconstruct state from Redis (Tier 1) or S3 (Tier 3)
    pm.reconstructState(pmCtx)

    // 2. Subscribe to upload events for this partition
    pm.coord.bus.SubscribePartitionUploads(pmCtx, pm.partitionID, pm.handleUploadEvent)

    // 3. Subscribe to task completion events for this partition
    pm.coord.bus.SubscribeCompletionEvents(pmCtx, pm.partitionID, pm.handleCompletionEvent)
}

func (pm *PartitionManager) Stop() {
    if pm.cancelFn != nil {
        pm.cancelFn()
    }
}

// reconstructState rebuilds the partition's active job state using the 3-tier
// fallback strategy defined in HLD §4.3. (I-13 fix)
//   Tier 1: Redis fast path (<50ms) — read active_jobs set + per-job status
//   Tier 3: S3 full scan (5-30s) — list all job dirs, skip completed, rebuild Redis
func (pm *PartitionManager) reconstructState(ctx context.Context) {
    // ──── Tier 1: Redis Fast Path ────
    jobIDs, err := pm.coord.state.GetActiveJobs(ctx, pm.partitionID)
    if err != nil || len(jobIDs) == 0 {
        // Redis unavailable or empty → fall through to S3 scan
        pm.reconstructFromS3(ctx)
        return
    }

    for _, jobID := range jobIDs {
        status, err := pm.coord.state.GetJobStatus(ctx, jobID)
        if err != nil {
            continue
        }

        phase := status["state"]
        // Skip terminal states
        if phase == string(JobPhaseCompleted) || phase == string(JobPhaseFailed) {
            pm.coord.state.RemoveActiveJob(ctx, pm.partitionID, jobID)
            continue
        }

        // ──── Tier 2: Backfill manifest cache if missing ────
        _, cacheErr := pm.coord.state.GetCachedManifest(ctx, jobID)
        if cacheErr != nil {
            manifest, mErr := pm.loadManifest(ctx, jobID)
            if mErr == nil {
                data, _ := json.Marshal(manifest)
                pm.coord.state.CacheManifest(ctx, jobID, data)
            }
        }

        // ──── Check if job completed while partition was orphaned ────
        total := parseInt(status["total"])
        if total > 0 {
            count, _ := pm.coord.state.BitCount(ctx, jobID)
            if int(count) >= total {
                // All tasks done — trigger manifest compilation if not already done
                first, _ := pm.coord.state.DeduplicateEvent(ctx, jobID+":manifest")
                if first {
                    go pm.compileManifests(ctx, jobID, total)
                }
            }
        }
    }
    log.Infof("partition %d: reconstructed %d active jobs from Redis", pm.partitionID, len(jobIDs))
}

// reconstructFromS3 is the Tier 3 fallback that scans S3 for active jobs.
// This is only used when Redis is unavailable or the partition has never been
// populated in Redis (first-time adoption after cluster bootstrap).
func (pm *PartitionManager) reconstructFromS3(ctx context.Context) {
    prefix := fmt.Sprintf("jobs/partition_%d/", pm.partitionID)
    keys, err := pm.coord.objStore.ListObjectsPrefix(ctx, prefix)
    if err != nil {
        log.Errorf("partition %d: S3 reconstruction failed: %v", pm.partitionID, err)
        return
    }

    seen := make(map[string]bool)
    for _, key := range keys {
        jobID := extractJobID(key)
        if seen[jobID] {
            continue
        }
        seen[jobID] = true

        // Check for completion sentinel
        sentinelKey := fmt.Sprintf("jobs/partition_%d/job_%s/job_completed.json", pm.partitionID, jobID)
        meta, _ := pm.coord.objStore.HeadObject(ctx, sentinelKey)
        if meta.Exists {
            continue // job already completed — skip
        }

        // Active job found — rebuild Redis state
        pm.coord.state.AddActiveJob(ctx, pm.partitionID, jobID)

        manifest, mErr := pm.loadManifest(ctx, jobID)
        if mErr != nil {
            continue
        }
        data, _ := json.Marshal(manifest)
        pm.coord.state.CacheManifest(ctx, jobID, data)

        // Count transcoded segments to rebuild bitmap
        transPrefix := fmt.Sprintf("jobs/partition_%d/job_%s/transcoded/", pm.partitionID, jobID)
        transKeys, _ := pm.coord.objStore.ListObjectsPrefix(ctx, transPrefix)
        completed := len(transKeys)

        pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
            "state":        string(JobPhaseTranscoding),
            "completed":    completed,
            "total":        manifest.TotalTasks,
            "partition":    pm.partitionID,
            "last_updated": time.Now().Unix(),
        })

        // Rebuild bitmap from S3 object existence
        for _, tk := range transKeys {
            seg, res := parseSegmentKey(tk) // extract segment index and resolution
            bitIdx := seg*len(AllResolutions) + resolutionOffset(res)
            pm.coord.state.SetBit(ctx, jobID, bitIdx)
        }

        // Check if reconstruction reveals a completed job
        if completed >= manifest.TotalTasks {
            first, _ := pm.coord.state.DeduplicateEvent(ctx, jobID+":manifest")
            if first {
                go pm.compileManifests(ctx, jobID, manifest.TotalTasks)
            }
        }
    }
    log.Infof("partition %d: reconstructed %d active jobs from S3", pm.partitionID, len(seen))
}

func (pm *PartitionManager) handleUploadEvent(msg TaskMessage) {
    jobID := extractJobID(msg.Data())

    // Deduplicate SQS at-least-once delivery
    isFirst, _ := pm.coord.state.DeduplicateEvent(context.Background(), jobID)
    if !isFirst {
        msg.Ack()
        return
    }

    // B-4 fix: ACK immediately before entering the semaphore queue.
    // If we defer ACK inside the goroutine, the semaphore wait can exceed
    // NATS AckWait (30s) during peak load, causing duplicate redelivery.
    // Safety: dedup check above + etcd slicing lock in sliceAndDispatch()
    // prevent any duplicate processing.
    msg.Ack()

    // Acquire slicing semaphore (blocks if 50 slots full)
    pm.coord.sliceSem <- struct{}{}
    go func() {
        defer func() { <-pm.coord.sliceSem }()
        pm.sliceAndDispatch(context.Background(), jobID)
    }()
}

func (pm *PartitionManager) sliceAndDispatch(ctx context.Context, jobID string) {
    // 1. Acquire etcd slicing lock
    acquired, _ := pm.coord.coord.AcquireSlicingLock(ctx, jobID, pm.coord.nodeID,
        pm.coord.cfg.Coordinator.SlicingLockTTLSec)
    if !acquired {
        return // another coordinator is already slicing
    }
    defer pm.coord.coord.ReleaseSlicingLock(ctx, jobID)

    // 2. Update phase to SLICING
    pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
        "state": string(JobPhaseSlicing), "last_updated": time.Now().Unix(),
    })
    pm.coord.state.PublishProgress(ctx, jobID, ProgressUpdate{Phase: JobPhaseSlicing})

    // 3. Validate input with ffprobe
    // 4. Handle moov atom (faststart) if needed
    // 5. Stream-slice via ffmpeg -c copy (see ingestion_slicing_design.md)
    segmentCount, err := pm.executeSlicing(ctx, jobID)
    if err != nil {
        pm.markJobFailed(ctx, jobID, err.Error())
        return
    }

    // 6. Update manifest with segment count
    totalTasks := segmentCount * len(AllResolutions)
    pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
        "state": string(JobPhaseTranscoding), "total": totalTasks,
        "last_updated": time.Now().Unix(),
    })

    // 7. Dispatch all tasks via NATS JetStream async publish
    for seg := 0; seg < segmentCount; seg++ {
        for _, res := range AllResolutions {
            task := SegmentTask{
                JobID:       jobID,
                PartitionID: pm.partitionID,
                OwnerEpoch:  pm.coord.currentEpoch, // D-2 fix: populate epoch for fencing
                SegmentIdx:  seg,
                Resolution:  res,
                RawChunkKey: fmt.Sprintf("jobs/partition_%d/job_%s/raw/chunk_%03d.mp4", pm.partitionID, jobID, seg),
                OutputKey:   fmt.Sprintf("jobs/partition_%d/job_%s/transcoded/segment_%03d_%s.ts", pm.partitionID, jobID, seg, res),
                HWAccel:     pm.coord.cfg.Worker.HWAccel,
                Priority:    "normal",
            }
            payload, _ := json.Marshal(task)
            // D-7: partition-to-shard mapping (requires PartitionCount % NATSShardCount == 0)
            shard := pm.partitionID / (pm.coord.cfg.Coordinator.PartitionCount / pm.coord.cfg.Coordinator.NATSShardCount)
            pm.coord.bus.PublishTaskAsync(ctx, shard, task.Priority, payload)
        }
    }
    // Flush all async publishes (blocks until NATS confirms all)
    pm.coord.bus.FlushPendingPublishes(ctx)
    pm.coord.state.PublishProgress(ctx, jobID, ProgressUpdate{Phase: JobPhaseTranscoding, Total: totalTasks})
}

func (pm *PartitionManager) handleCompletionEvent(msg TaskMessage) {
    var task SegmentTask
    json.Unmarshal(msg.Data(), &task)
    msg.Ack()

    // Check BITCOUNT → if all tasks done, compile manifests
    count, _ := pm.coord.state.BitCount(context.Background(), task.JobID)
    status, _ := pm.coord.state.GetJobStatus(context.Background(), task.JobID)
    total := parseInt(status["total"])

    if int(count) >= total && total > 0 {
        // D-6 fix: Prevent concurrent manifest compilations from multiple
        // completion events passing BITCOUNT check simultaneously
        first, _ := pm.coord.state.DeduplicateEvent(context.Background(), task.JobID+":manifest")
        if first {
            pm.compileManifests(context.Background(), task.JobID, total)
        }
    }
}

func (pm *PartitionManager) compileManifests(ctx context.Context, jobID string, totalTasks int) {
    // I-3 fix: Epoch fencing — abort if we are a stale coordinator
    status, _ := pm.coord.state.GetJobStatus(ctx, jobID)
    if status["owner_epoch"] != "" {
        storedEpoch := parseInt64(status["owner_epoch"])
        if storedEpoch > pm.coord.currentEpoch {
            log.Warnf("epoch fencing: stale coordinator (ours=%d, stored=%d), aborting manifest for %s",
                pm.coord.currentEpoch, storedEpoch, jobID)
            return
        }
    }

    // 1. Update phase
    pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
        "state": string(JobPhaseCompiling), "last_updated": time.Now().Unix(),
        "owner_epoch": pm.coord.currentEpoch,
    })

    // 2. Consistency barrier: wait 1s for S3 eventual consistency
    time.Sleep(1 * time.Second)

    // 3. Verify last segment exists via HeadObject
    // 4. Read durations from Redis
    durations, _ := pm.coord.state.GetAllDurations(ctx, jobID)

    // 5. Generate HLS master + media playlists
    hlsManifest := pm.generateHLS(jobID, durations)

    // 6. Generate DASH MPD
    dashManifest := pm.generateDASH(jobID, durations)

    // 7. Write to S3
    prefix := fmt.Sprintf("jobs/partition_%d/job_%s/", pm.partitionID, jobID)
    pm.coord.objStore.PutObject(ctx, prefix+"master.m3u8", hlsManifest, int64(hlsManifest.Len()))
    pm.coord.objStore.PutObject(ctx, prefix+"manifest.mpd", dashManifest, int64(dashManifest.Len()))
    pm.coord.objStore.PutObject(ctx, prefix+"job_completed.json", completedSentinel(), 2)

    // 8. Notify client
    pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
        "state": string(JobPhaseCompleted), "last_updated": time.Now().Unix(),
    })
    pm.coord.state.PublishProgress(ctx, jobID, ProgressUpdate{
        Phase:   JobPhaseCompleted,
        HLSURL:  fmt.Sprintf("https://cdn.example.com/%smaster.m3u8", prefix),
        DASHURL: fmt.Sprintf("https://cdn.example.com/%smanifest.mpd", prefix), // D-8 fix
    })

    // 9. Cleanup: remove from active jobs
    pm.coord.state.RemoveActiveJob(ctx, pm.partitionID, jobID)
}
```

---

## 6. Worker Daemon Architecture

### 6.1 Internal Goroutine Model

```
Worker Process
├── Task Puller (1 goroutine per NATS shard)
│   └── Pulls tasks via JetStream PullSubscribe
│       → Dispatches to task executor pool via Go channel
│
├── Task Executor Pool (N goroutines, N = ConcurrentTasks config)
│   ├── Each executor:
│   │   ├── Disk quota check (syscall.Statfs)
│   │   ├── Two-tier idempotency check (Redis → S3 HEAD)
│   │   ├── Download raw chunk from S3 to /tmp/scratch
│   │   ├── Spawn FFmpeg subprocess (with cgroups + Pdeathsig)
│   │   ├── Launch watchdog goroutine (runtime.LockOSThread)
│   │   ├── Send msg.InProgress() every 10s
│   │   ├── Upload output .ts to S3
│   │   ├── Execute Redis completion pipeline
│   │   └── ACK NATS task
│   │
│   └── Circuit Breaker (shared across all executors)
│       └── Tracks Redis failure rate → opens after 3 failures in 5s
│
├── Scratch GC (1 background goroutine)
│   └── Periodically cleans orphaned temp files from /tmp/scratch
│
├── Metrics Server (:9090 /metrics)
│
└── Graceful Shutdown Handler (SIGTERM → 5 min drain)
```

### 6.2 Task Executor with Watchdog

```go
package worker

import (
    "context"
    "os"
    "os/exec"
    "runtime"
    "syscall"
    "time"
)

type TaskExecutor struct {
    state     StateStore
    objStore  ObjectStore
    cfg       WorkerConfig
    breaker   *CircuitBreaker
}

func (te *TaskExecutor) Execute(ctx context.Context, msg TaskMessage, task SegmentTask) error {
    // ──── Step 1: Disk Quota Check ────
    var stat syscall.Statfs_t
    syscall.Statfs(te.cfg.ScratchDir, &stat)
    freeGB := (stat.Bavail * uint64(stat.Bsize)) / (1024 * 1024 * 1024)
    if freeGB < uint64(te.cfg.MinDiskFreeGB) {
        msg.Nak() // re-queue to another worker
        return fmt.Errorf("disk quota exceeded: %d GB free", freeGB)
    }

    // ──── Step 2: Two-Tier Idempotency Check ────
    if te.checkIdempotency(ctx, task) {
        msg.Ack() // already completed
        return nil
    }

    // ──── Step 3: Download raw chunk ────
    localInput := filepath.Join(te.cfg.ScratchDir, fmt.Sprintf("%s_%d.mp4", task.JobID, task.SegmentIdx))
    if err := te.downloadFromS3(ctx, task.RawChunkKey, localInput); err != nil {
        msg.Nak()
        return err
    }
    defer os.Remove(localInput)

    // ──── Step 4: Transcode with FFmpeg ────
    localOutput := filepath.Join(te.cfg.ScratchDir, fmt.Sprintf("%s_%d_%s.ts", task.JobID, task.SegmentIdx, task.Resolution))
    defer os.Remove(localOutput)

    transcodeCtx, transcodeCancel := context.WithTimeout(ctx, time.Duration(te.cfg.MaxTaskDurationMin)*time.Minute)
    defer transcodeCancel()

    ffmpegArgs := te.buildFFmpegArgs(localInput, localOutput, task.Resolution)
    cmd := exec.CommandContext(transcodeCtx, "ffmpeg", ffmpegArgs...)
    cmd.SysProcAttr = platformSysProcAttr()

    // Launch macOS parent-death watchdog (no-op on Linux where Pdeathsig handles it)
    go platformParentWatchdog(transcodeCtx, cmd)

    // ──── Step 5: Launch Watchdog on dedicated OS thread ────
    watchdogDone := make(chan struct{})
    go func() {
        runtime.LockOSThread()
        defer close(watchdogDone)
        te.runWatchdog(transcodeCtx, cmd, localOutput)
    }()

    // ──── Step 6: InProgress heartbeat (extend NATS AckWait) ────
    heartbeatDone := make(chan struct{})
    go func() {
        defer close(heartbeatDone)
        ticker := time.NewTicker(10 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-transcodeCtx.Done():
                return
            case <-ticker.C:
                msg.InProgress() // reset AckWait deadline
            }
        }
    }()

    // ──── Step 7: Run FFmpeg ────
    if err := cmd.Run(); err != nil {
        <-watchdogDone
        <-heartbeatDone
        // Don't ACK — let NATS AckWait redeliver
        return fmt.Errorf("ffmpeg failed: %w", err)
    }
    transcodeCancel()
    <-watchdogDone
    <-heartbeatDone

    // ──── Step 8: Probe duration BEFORE upload (B-1 fix: file still on disk) ────
    duration := te.probeDuration(localOutput)

    // ──── Step 9: Upload to S3 (temporary path first) ────
    tempOutputKey := fmt.Sprintf("%s.%s.tmp", task.OutputKey, te.cfg.NodeID)
    f, _ := os.Open(localOutput)
    fi, _ := f.Stat()
    te.objStore.PutObject(ctx, tempOutputKey, f, fi.Size())
    f.Close()

    // Atomic rename to canonical path (prevents double-commit)
    te.objStore.CopyObject(ctx, tempOutputKey, task.OutputKey)
    te.objStore.DeleteObject(ctx, tempOutputKey)

    // ──── Step 10: Redis Completion Pipeline (single RTT) ────
    te.state.ExecuteCompletionPipeline(ctx, CompletionPipelineParams{
        JobID:      task.JobID,
        SegmentIdx: task.SegmentIdx,
        Resolution: string(task.Resolution),
        BitIndex:   task.BitIndex(),
        Duration:   duration,
        UnixNow:    time.Now().Unix(),
    })

    // ──── Step 11: ACK ────
    msg.Ack()
    return nil
}

// probeDuration runs ffprobe on the output file and returns the duration as a string
// suitable for Redis HSET (e.g., "5.005"). Returns "0" on error.
func (te *TaskExecutor) probeDuration(filePath string) string {
    out, err := exec.Command("ffprobe",
        "-v", "error",
        "-show_entries", "format=duration",
        "-of", "default=noprint_wrappers=1:nokey=1",
        filePath,
    ).Output()
    if err != nil {
        return "0"
    }
    return strings.TrimSpace(string(out))
}

func (te *TaskExecutor) runWatchdog(ctx context.Context, cmd *exec.Cmd, outputPath string) {
    ticker := time.NewTicker(time.Duration(te.cfg.WatchdogIntervalSec) * time.Second)
    defer ticker.Stop()

    startTime := time.Now()
    var lastSize int64
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // 1. Check max execution duration (I-14 fix)
            if time.Since(startTime) > time.Duration(te.cfg.MaxTaskDurationMin)*time.Minute {
                syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
                return
            }

            fi, err := os.Stat(outputPath)
            if err != nil {
                continue // file not yet created
            }
            currentSize := fi.Size()

            // 2. Check max temp file size (I-14 fix)
            maxSizeBytes := int64(te.cfg.MaxTempFileSizeGB) * 1024 * 1024 * 1024
            if currentSize > maxSizeBytes {
                syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
                return
            }

            // 3. Check for stalled process
            if currentSize == lastSize && lastSize > 0 {
                // No progress → FFmpeg stalled → kill it
                syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // kill process group
                return
            }
            lastSize = currentSize
        }
    }
}

func (te *TaskExecutor) buildFFmpegArgs(input, output string, res Resolution) []string {
    // Resolution presets
    presets := map[Resolution][]string{
        Res1080p: {"-vf", "scale=1920:1080", "-b:v", "5000k"},
        Res720p:  {"-vf", "scale=1280:720", "-b:v", "2800k"},
        Res480p:  {"-vf", "scale=854:480", "-b:v", "1400k"},
    }

    args := []string{"-i", input}

    // Hardware acceleration
    switch te.cfg.HWAccel {
    case "nvenc":
        args = append(args, "-hwaccel", "cuda", "-c:v", "h264_nvenc")
    case "vaapi":
        args = append(args, "-hwaccel", "vaapi", "-c:v", "h264_vaapi")
    case "videotoolbox":
        args = append(args, "-c:v", "h264_videotoolbox")
    default:
        args = append(args, "-c:v", "libx264", "-preset", "fast")
    }

    args = append(args, presets[res]...)
    args = append(args,
        "-c:a", "aac", "-b:a", "128k",
        "-copyts",                                    // preserve presentation timestamps
        "-force_key_frames", "expr:gte(t,0)",         // align keyframes across resolutions
        "-f", "mpegts",
        "-y", output,
    )
    return args
}
```

### 6.3 Circuit Breaker (Redis Failure Protection)

```go
package worker

import (
    "sync"
    "time"
)

// CircuitBreaker prevents a thundering herd of S3 HEAD requests
// when Redis is temporarily unreachable.
type CircuitBreaker struct {
    mu              sync.Mutex
    failures        []time.Time // timestamps of recent failures
    windowDuration  time.Duration
    threshold       int
    open            bool
    backoffBase     time.Duration
    backoffMax      time.Duration
    consecutiveFails int
}

func NewCircuitBreaker(windowSec, threshold int) *CircuitBreaker {
    return &CircuitBreaker{
        windowDuration: time.Duration(windowSec) * time.Second,
        threshold:      threshold,
        backoffBase:    100 * time.Millisecond,
        backoffMax:     5 * time.Second,
    }
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    now := time.Now()
    cb.failures = append(cb.failures, now)
    cb.consecutiveFails++

    // Trim old failures outside the window
    cutoff := now.Add(-cb.windowDuration)
    trimmed := cb.failures[:0]
    for _, t := range cb.failures {
        if t.After(cutoff) {
            trimmed = append(trimmed, t)
        }
    }
    cb.failures = trimmed

    if len(cb.failures) >= cb.threshold {
        cb.open = true
    }
}

func (cb *CircuitBreaker) RecordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.consecutiveFails = 0
    cb.open = false
}

// IsOpen returns true if Redis should not be contacted.
// When open, the caller must apply exponential backoff before S3 fallback.
func (cb *CircuitBreaker) IsOpen() bool {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    return cb.open
}

// BackoffDuration returns the current backoff duration based on consecutive failures.
func (cb *CircuitBreaker) BackoffDuration() time.Duration {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    d := cb.backoffBase
    for i := 0; i < cb.consecutiveFails-1 && d < cb.backoffMax; i++ {
        d *= 2
    }
    if d > cb.backoffMax {
        d = cb.backoffMax
    }
    return d
}
```

### 6.4 Two-Tier Idempotency Check

```go
func (te *TaskExecutor) checkIdempotency(ctx context.Context, task SegmentTask) bool {
    // Fast path: Redis EXISTS (< 0.1ms)
    if !te.breaker.IsOpen() {
        exists, err := te.state.TaskExists(ctx, task.JobID, task.SegmentIdx, string(task.Resolution))
        if err != nil {
            te.breaker.RecordFailure()
            // Fall through to S3
        } else {
            te.breaker.RecordSuccess()
            if exists {
                return true // already done
            }
            return false // not done, proceed with transcoding
        }
    }

    // Circuit breaker is open — apply backoff before S3 fallback
    if te.breaker.IsOpen() {
        time.Sleep(te.breaker.BackoffDuration())
    }

    // Slow path: S3 HeadObject (5-10ms)
    meta, err := te.objStore.HeadObject(ctx, task.OutputKey)
    if err != nil {
        return false // assume not done
    }
    return meta.Exists
}
```

### 6.5 Graceful Drain (SIGTERM Handler)

```go
package worker

import (
    "context"
    "time"
)

func (w *WorkerDaemon) Run(ctx context.Context) {
    // ... start task pullers and executor pool ...

    <-ctx.Done() // SIGTERM received

    // 1. Stop pulling new tasks from NATS
    w.stopPullers()

    // 2. Wait for in-flight tasks to complete (up to 5 minutes)
    // B-3 fix: race wg.Wait() against drain timeout via goroutine
    drainCtx, drainCancel := context.WithTimeout(context.Background(),
        time.Duration(w.cfg.Worker.GracefulDrainSec)*time.Second)
    defer drainCancel()

    done := make(chan struct{})
    go func() {
        w.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        // All tasks completed within drain window
    case <-drainCtx.Done():
        // Timeout: kill remaining FFmpeg processes
        w.killAllFFmpeg()
        // Unacked tasks will be redelivered by NATS AckWait
    }
}
```

---

## 7. Redis Completion Pipeline Implementation

The completion pipeline is the most latency-sensitive path in the system. It must execute atomically in a single Redis round-trip to prevent inconsistencies.

```go
package infra

import (
    "context"
    "fmt"
    "github.com/redis/go-redis/v9"
)

// ExecuteCompletionPipeline executes the worker's post-transcode
// state update in a single Redis pipeline (single network RTT).
//
// CRITICAL: All keys use Hash Tags {job_uuid} to prevent CROSSSLOT
// errors in Redis Cluster. The keys are:
//   task:{job_uuid}:seg:res      → SET (idempotency flag)
//   job:{job_uuid}:progress      → SETBIT (bitmap)
//   job:{job_uuid}:status        → HINCRBY + HSET (counter + timestamp)
//   job:{job_uuid}:durations     → HSET (segment duration)
//   progress:{job_uuid}          → XADD (stream for WebSocket)
func (r *RedisStateStore) ExecuteCompletionPipeline(ctx context.Context, p CompletionPipelineParams) error {
    // Hash Tag ensures all keys route to the same Redis shard
    taskKey     := fmt.Sprintf("task:{%s}:%d:%s", p.JobID, p.SegmentIdx, p.Resolution)
    progressKey := fmt.Sprintf("job:{%s}:progress", p.JobID)
    statusKey   := fmt.Sprintf("job:{%s}:status", p.JobID)
    durationsKey := fmt.Sprintf("job:{%s}:durations", p.JobID)
    streamKey   := fmt.Sprintf("progress:{%s}", p.JobID)

    pipe := r.client.Pipeline()

    // 1. Idempotency flag (TTL 24h)
    pipe.Set(ctx, taskKey, "1", 24*time.Hour)

    // 2. Progress bitmap
    pipe.SetBit(ctx, progressKey, int64(p.BitIndex), 1)

    // 3. Job status counter
    pipe.HIncrBy(ctx, statusKey, "completed", 1)
    pipe.HSet(ctx, statusKey, "last_updated", p.UnixNow)

    // 4. Segment duration for manifest compilation
    segResKey := fmt.Sprintf("segment_%03d_%s", p.SegmentIdx, p.Resolution)
    pipe.HSet(ctx, durationsKey, segResKey, p.Duration)

    // 5. Progress stream for WebSocket delivery
    pipe.XAdd(ctx, &redis.XAddArgs{
        Stream: streamKey,
        MaxLen: 1000,
        Approx: true,
        Values: map[string]interface{}{
            "phase":     "TRANSCODING",
            "completed": p.Completed + 1,
            "total":     p.Total,
        },
    })

    _, err := pipe.Exec(ctx)
    return err
}
```

---

## 8. Shared Partition Hashing (D-1 Fix)

To guarantee that the Gateway and Coordinator always resolve the same `jobID → partitionID` mapping, the hash function lives in the shared `models` package.

```go
package models

import "hash/fnv"

// PartitionOf deterministically maps a Job UUID to a partition.
// Used by both Gateway (to set the S3 path prefix) and Coordinator
// (to validate incoming events belong to an owned partition).
// Algorithm: FNV-1a of the raw job UUID string, mod totalPartitions.
func PartitionOf(jobID string, totalPartitions int) int {
    h := fnv.New32a()
    h.Write([]byte(jobID))
    return int(h.Sum32()) % totalPartitions
}
```

Both `GatewayDaemon.CreateUploadSession()` and `PartitionManager.handleUploadEvent()` MUST call `models.PartitionOf()` instead of inlining the hash. This prevents silent divergence if one side changes the algorithm.

---

## 9. Redis Hash Tag Key Builder (D-4 Fix)

To enforce that all Redis keys include `{job_uuid}` Hash Tags (preventing CROSSSLOT pipeline errors), all key construction goes through a centralized builder.

```go
package infra

import "fmt"

// RedisKeys constructs all Redis keys for a given job UUID.
// All keys include {job_uuid} as a Hash Tag, ensuring they
// route to the same Redis Cluster shard for pipeline safety.
type RedisKeys struct {
    JobID string
}

func NewRedisKeys(jobID string) RedisKeys {
    return RedisKeys{JobID: jobID}
}

func (k RedisKeys) StatusHash() string {
    return fmt.Sprintf("job:{%s}:status", k.JobID)
}

func (k RedisKeys) ProgressBitmap() string {
    return fmt.Sprintf("job:{%s}:progress", k.JobID)
}

func (k RedisKeys) DurationsHash() string {
    return fmt.Sprintf("job:{%s}:durations", k.JobID)
}

func (k RedisKeys) ManifestCache() string {
    return fmt.Sprintf("job:{%s}:manifest", k.JobID)
}

func (k RedisKeys) ProgressStream() string {
    return fmt.Sprintf("progress:{%s}", k.JobID)
}

func (k RedisKeys) TaskIdempotency(segmentIdx int, resolution string) string {
    return fmt.Sprintf("task:{%s}:%d:%s", k.JobID, segmentIdx, resolution)
}

func (k RedisKeys) DeduplicationEvent() string {
    return fmt.Sprintf("upload:event:{%s}", k.JobID)
}
```

All `StateStore` method implementations MUST use `RedisKeys` instead of `fmt.Sprintf` to construct keys. This makes Hash Tag compliance compile-time enforced.

---

## 10. cgroups v2 Resource Fencing (I-1 Fix)

Per DTDP §2.2, FFmpeg subprocesses must be constrained via cgroups v2 to prevent resource exhaustion on shared worker nodes.

```go
package worker

import (
    "fmt"
    "os"
    "path/filepath"
)

const cgroupBase = "/sys/fs/cgroup"

// CgroupFencer creates and configures a cgroup v2 scope for an FFmpeg process.
type CgroupFencer struct {
    ScratchDir    string
    MemoryLimitMB int // 1536 (1.5 GB per DTDP)
    CPUWeight     int // 100 (default fair share)
}

// CreateScope creates a cgroup v2 scope directory and sets resource limits.
// Returns the cgroup directory path for the process to join.
func (cf *CgroupFencer) CreateScope(jobID string, segIdx int) (string, error) {
    scopeName := fmt.Sprintf("transcoder-%s-%d.scope", jobID, segIdx)
    scopePath := filepath.Join(cgroupBase, "transcoder.slice", scopeName)

    if err := os.MkdirAll(scopePath, 0755); err != nil {
        return "", fmt.Errorf("failed to create cgroup scope: %w", err)
    }

    // Set memory limit (hard ceiling — OOM-kill if exceeded)
    memMax := fmt.Sprintf("%d", cf.MemoryLimitMB*1024*1024)
    os.WriteFile(filepath.Join(scopePath, "memory.max"), []byte(memMax), 0644)

    // Set CPU weight (proportional share)
    os.WriteFile(filepath.Join(scopePath, "cpu.weight"), []byte(fmt.Sprintf("%d", cf.CPUWeight)), 0644)

    return scopePath, nil
}

// JoinScope moves a PID into the cgroup scope.
func (cf *CgroupFencer) JoinScope(scopePath string, pid int) error {
    return os.WriteFile(filepath.Join(scopePath, "cgroup.procs"), []byte(fmt.Sprintf("%d", pid)), 0644)
}

// CleanupScope removes the cgroup scope directory after the process exits.
func (cf *CgroupFencer) CleanupScope(scopePath string) error {
    return os.Remove(scopePath)
}
```

In `TaskExecutor.Execute()`, after `cmd.Start()`:
```go
// After cmd.Start() — move FFmpeg into its cgroup
fencer := &CgroupFencer{MemoryLimitMB: 1536, CPUWeight: 100}
scopePath, _ := fencer.CreateScope(task.JobID, task.SegmentIdx)
fencer.JoinScope(scopePath, cmd.Process.Pid)
defer fencer.CleanupScope(scopePath)
```

> **Note**: cgroups v2 requires Linux. On macOS (Apple Silicon VPU workers), resource limits are enforced via `ulimit` or macOS sandbox profiles instead.

---

## 11. Platform-Specific Build Tags (I-2 Fix)

`Pdeathsig` is Linux-only. To prevent compile errors on macOS and ensure correct orphan process cleanup, platform-specific files are used.

### `executor_linux.go`
```go
//go:build linux

package worker

import "syscall"

// platformSysProcAttr returns the SysProcAttr for Linux with Pdeathsig.
func platformSysProcAttr() *syscall.SysProcAttr {
    return &syscall.SysProcAttr{
        Setpgid:   true,
        Pdeathsig: syscall.SIGKILL, // Kill FFmpeg if parent worker dies
    }
}

// platformParentWatchdog is a no-op on Linux (Pdeathsig handles it).
func platformParentWatchdog(ctx context.Context, cmd *exec.Cmd) {}
```

### `executor_darwin.go`
```go
//go:build darwin

package worker

import (
    "context"
    "os"
    "os/exec"
    "syscall"
    "time"
)

// platformSysProcAttr returns the SysProcAttr for macOS (no Pdeathsig).
func platformSysProcAttr() *syscall.SysProcAttr {
    return &syscall.SysProcAttr{
        Setpgid: true,
    }
}

// platformParentWatchdog monitors the parent PID on macOS.
// If the parent changes to PID 1 (launchd), FFmpeg is orphaned and must be killed.
func platformParentWatchdog(ctx context.Context, cmd *exec.Cmd) {
    originalParent := os.Getppid()
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if os.Getppid() != originalParent {
                // Parent died — kill FFmpeg process group
                syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
                return
            }
        }
    }
}
```

In `TaskExecutor.Execute()`, replace the hardcoded `SysProcAttr`:
```go
cmd.SysProcAttr = platformSysProcAttr()
go platformParentWatchdog(transcodeCtx, cmd) // no-op on Linux
```

---

## 12. Job Garbage Collection Daemon (I-4 Fix)

Per DTDP §5.8, orphaned jobs accumulate ~2.5 TB/day without GC. This daemon runs on the coordinator tier.

```go
package coordinator

import (
    "context"
    "time"
)

// JobGCDaemon scans owned partitions for stale/abandoned jobs
// and cleans up S3 objects and Redis state.
type JobGCDaemon struct {
    coord          *CoordinatorDaemon
    intervalMin    int // 10
    staleThreshSec int64 // 86400 (24 hours)
}

func (gc *JobGCDaemon) Run(ctx context.Context) {
    ticker := time.NewTicker(time.Duration(gc.intervalMin) * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            gc.scanOwnedPartitions(ctx)
        }
    }
}

func (gc *JobGCDaemon) scanOwnedPartitions(ctx context.Context) {
    gc.coord.mu.Lock()
    partitionIDs := make([]int, 0, len(gc.coord.partitions))
    for pid := range gc.coord.partitions {
        partitionIDs = append(partitionIDs, pid)
    }
    gc.coord.mu.Unlock()

    for _, pid := range partitionIDs {
        gc.scanPartition(ctx, pid)
    }
}

func (gc *JobGCDaemon) scanPartition(ctx context.Context, partitionID int) {
    // 1. Get active jobs from Redis SET
    jobIDs, err := gc.coord.state.GetActiveJobs(ctx, partitionID)
    if err != nil {
        return
    }

    now := time.Now().Unix()

    for _, jobID := range jobIDs {
        // 2. Check last_updated timestamp
        status, err := gc.coord.state.GetJobStatus(ctx, jobID)
        if err != nil {
            continue
        }

        lastUpdated := parseInt64(status["last_updated"])
        phase := status["state"]

        // 3. Skip completed jobs
        if phase == string(JobPhaseCompleted) {
            continue
        }

        // 4. Check if job is stale (no updates in 24 hours)
        if now-lastUpdated < gc.staleThreshSec {
            continue
        }

        // 5. Mark as ABANDONED
        log.Warnf("GC: abandoning stale job %s (last updated %ds ago)", jobID, now-lastUpdated)

        // 6. Delete S3 objects
        prefix := fmt.Sprintf("jobs/partition_%d/job_%s/", partitionID, jobID)
        gc.coord.objStore.DeletePrefix(ctx, prefix)

        // 7. Cleanup Redis state
        gc.coord.state.RemoveActiveJob(ctx, partitionID, jobID)
        // Individual key cleanup: status, progress bitmap, durations, manifest cache
        keys := NewRedisKeys(jobID)
        gc.coord.state.DeleteKeys(ctx,
            keys.StatusHash(),
            keys.ProgressBitmap(),
            keys.DurationsHash(),
            keys.ManifestCache(),
            keys.ProgressStream(),
        )

        // 8. Notify client via WebSocket
        gc.coord.state.PublishProgress(ctx, jobID, ProgressUpdate{
            Phase: JobPhaseFailed,
            Error: "job abandoned: no progress for 24 hours",
        })
    }
}
```

> **Note**: The `StateStore` interface should be extended with `DeleteKeys(ctx context.Context, keys ...string) error` for GC cleanup.

---

## 13. Observability Configuration & Registration (I-5 Fix)

### 13.1 Configuration Structs

```go
package config

type MetricsConfig struct {
    Enabled    bool   `yaml:"enabled"`     // true
    ListenAddr string `yaml:"listen_addr"` // ":9090"
    Path       string `yaml:"path"`        // "/metrics"
}

type TracingConfig struct {
    Enabled       bool    `yaml:"enabled"`        // true
    CollectorURL  string  `yaml:"collector_url"`  // "otel-collector:4317"
    SamplingRate  float64 `yaml:"sampling_rate"`   // 0.01 (1% steady, 1.0 for DLQ jobs)
    ServiceName   string  `yaml:"service_name"`    // "transcoder-gateway" etc.
}
```

### 13.2 Prometheus Metric Registration

```go
package metrics

import "github.com/prometheus/client_golang/prometheus"

// GatewayMetrics contains all Prometheus metrics for the Gateway tier.
type GatewayMetrics struct {
    UploadRequests     prometheus.Counter
    UploadBytes        prometheus.Counter
    ActiveWebSockets   prometheus.Gauge
    PresignedURLLatency prometheus.Histogram
    RateLimitRejects   prometheus.Counter
}

func NewGatewayMetrics(reg prometheus.Registerer) *GatewayMetrics {
    m := &GatewayMetrics{
        UploadRequests: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "gateway_upload_requests_total",
        }),
        UploadBytes: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "gateway_upload_bytes_total",
        }),
        ActiveWebSockets: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "gateway_active_websockets",
        }),
        PresignedURLLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    "gateway_presigned_url_latency_ms",
            Buckets: []float64{1, 5, 10, 25, 50, 100, 200, 500},
        }),
        RateLimitRejects: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "gateway_rate_limit_rejections_total",
        }),
    }
    reg.MustRegister(m.UploadRequests, m.UploadBytes, m.ActiveWebSockets,
        m.PresignedURLLatency, m.RateLimitRejects)
    return m
}

// CoordinatorMetrics contains all Prometheus metrics for the Coordinator tier.
type CoordinatorMetrics struct {
    ActiveJobs         prometheus.Gauge
    SlicingBacklog     prometheus.Gauge
    SlicingDuration    prometheus.Histogram
    ManifestDuration   prometheus.Histogram
    BitcountLatency    prometheus.Histogram
    PartitionAdoptions prometheus.Counter
    DLQDepth           prometheus.Gauge
    GCOrphanedJobs     prometheus.Counter
}

func NewCoordinatorMetrics(reg prometheus.Registerer) *CoordinatorMetrics {
    m := &CoordinatorMetrics{
        ActiveJobs: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "coord_active_jobs",
        }),
        SlicingBacklog: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "coord_slicing_backlog",
        }),
        SlicingDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    "coord_slicing_duration_seconds",
            Buckets: prometheus.DefBuckets,
        }),
        ManifestDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    "coord_manifest_compilation_seconds",
            Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
        }),
        BitcountLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    "coord_bitcount_latency_ms",
            Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
        }),
        PartitionAdoptions: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "coord_partition_adoptions_total",
        }),
        DLQDepth: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "coord_dlq_depth",
        }),
        GCOrphanedJobs: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "coord_gc_orphaned_jobs_total",
        }),
    }
    reg.MustRegister(m.ActiveJobs, m.SlicingBacklog, m.SlicingDuration,
        m.ManifestDuration, m.BitcountLatency, m.PartitionAdoptions,
        m.DLQDepth, m.GCOrphanedJobs)
    return m
}

// WorkerMetrics contains all Prometheus metrics for the Worker tier.
type WorkerMetrics struct {
    TranscodeDuration  prometheus.Histogram
    FFmpegCrashes      prometheus.Counter
    IdempotencyHits    prometheus.Counter
    S3FallbackTotal    prometheus.Counter
    CircuitBreakerOpen prometheus.Gauge
    DiskFreeBytes      prometheus.Gauge
    NATSInflightTasks  prometheus.Gauge
    GPUUtilization     prometheus.Gauge
}

func NewWorkerMetrics(reg prometheus.Registerer) *WorkerMetrics {
    m := &WorkerMetrics{
        TranscodeDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    "worker_transcode_duration_seconds",
            Buckets: []float64{1, 5, 10, 20, 30, 60, 120},
        }),
        FFmpegCrashes: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "worker_ffmpeg_crashes_total",
        }),
        IdempotencyHits: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "worker_idempotency_hits_total",
        }),
        S3FallbackTotal: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "worker_s3_fallback_total",
        }),
        CircuitBreakerOpen: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "worker_circuit_breaker_open",
        }),
        DiskFreeBytes: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "worker_disk_free_bytes",
        }),
        NATSInflightTasks: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "worker_nats_inflight_tasks",
        }),
        GPUUtilization: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "worker_gpu_utilization_pct",
        }),
    }
    reg.MustRegister(m.TranscodeDuration, m.FFmpegCrashes, m.IdempotencyHits,
        m.S3FallbackTotal, m.CircuitBreakerOpen, m.DiskFreeBytes,
        m.NATSInflightTasks, m.GPUUtilization)
    return m
}
```

### 13.3 OpenTelemetry Trace Initialization

```go
package tracing

import (
    "context"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// InitTracer initializes the OpenTelemetry tracer with OTLP gRPC exporter.
// trace_id is derived from Job_UUID for end-to-end correlation.
func InitTracer(ctx context.Context, serviceName, collectorURL string, samplingRate float64) (*sdktrace.TracerProvider, error) {
    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(collectorURL),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithSampler(sdktrace.TraceIDRatioBased(samplingRate)),
        sdktrace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName(serviceName),
        )),
    )
    otel.SetTracerProvider(tp)
    return tp, nil
}
```

---

## 14. DLQ Monitor (I-6 Fix)

Per DTDP §3.4, tasks that fail `MaxDeliver = 3` times route to `transcode-tasks-dlq`. The coordinator monitors this queue and marks affected jobs as failed.

```go
package coordinator

import (
    "context"
    "encoding/json"
    "fmt"
)

// runDLQMonitor subscribes to the Dead Letter Queue and marks jobs as FAILED
// when their tasks exhaust all retry attempts.
func (c *CoordinatorDaemon) runDLQMonitor(ctx context.Context) {
    c.bus.SubscribeDLQ(ctx, func(msg TaskMessage) {
        var task SegmentTask
        if err := json.Unmarshal(msg.Data(), &task); err != nil {
            msg.Ack() // drop malformed messages
            return
        }
        msg.Ack()

        meta := msg.Metadata()
        log.Errorf("DLQ: task job=%s seg=%d res=%s failed after %d attempts",
            task.JobID, task.SegmentIdx, task.Resolution, meta.NumDelivered)

        // Check if this job is owned by us (we only act on our partitions)
        if c.ring.OwnerOf(models.PartitionOf(task.JobID, c.cfg.Coordinator.PartitionCount)) != c.nodeID {
            return // not our partition — another coordinator will handle it
        }

        // Mark job as FAILED
        keys := NewRedisKeys(task.JobID)
        c.state.SetJobStatus(ctx, task.JobID, map[string]interface{}{
            "state":        string(JobPhaseFailed),
            "last_updated": time.Now().Unix(),
            "error":        fmt.Sprintf("segment %d %s failed after %d attempts", task.SegmentIdx, task.Resolution, meta.NumDelivered),
        })

        // Notify client via WebSocket
        c.state.PublishProgress(ctx, task.JobID, ProgressUpdate{
            Phase: JobPhaseFailed,
            Error: fmt.Sprintf("transcoding failed: segment %d %s exceeded retry limit",
                task.SegmentIdx, task.Resolution),
        })

        // Increment DLQ depth metric
        // metrics.DLQDepth.Inc()
        _ = keys // used by future cleanup
    })
}
```

The `runDLQMonitor` goroutine is launched alongside other coordinator goroutines in `CoordinatorDaemon.Run()`:
```go
go c.runDLQMonitor(ctx) // in CoordinatorDaemon.Run()
```

---

## 15. Slicer Implementation (I-8 Fix)

Per ISD §2.1, slicing uses FFmpeg stream-copy mode (`-c copy`) to segment raw uploads at GOP boundaries. This integrates the ISD's slicing mechanics into the `PartitionManager` context.

```go
package coordinator

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"
)

// executeSlicing streams the raw upload from S3, slices it into GOP-aligned
// segments via ffmpeg -c copy, and uploads each segment back to S3.
// Returns the segment count or an error.
func (pm *PartitionManager) executeSlicing(ctx context.Context, jobID string) (int, error) {
    // 1. Load job manifest from S3
    manifest, err := pm.loadManifest(ctx, jobID)
    if err != nil {
        return 0, fmt.Errorf("failed to load manifest: %w", err)
    }

    // 2. Validate input with ffprobe (ISD §3.2 — input sanitation)
    valid, err := pm.validateInput(ctx, manifest.SourcePath)
    if err != nil || !valid {
        return 0, fmt.Errorf("input validation failed: %w", err)
    }

    // 3. Check moov atom position (ISD §3.1 — non-faststart recovery)
    needsFaststart, err := pm.checkMoovAtom(ctx, manifest.SourcePath)
    if err != nil {
        return 0, fmt.Errorf("moov atom check failed: %w", err)
    }
    if needsFaststart {
        if err := pm.runFaststart(ctx, jobID, manifest.SourcePath); err != nil {
            return 0, fmt.Errorf("faststart failed: %w", err)
        }
    }

    // 4. Create temp directory for segments
    tempDir, err := os.MkdirTemp(pm.coord.cfg.Worker.ScratchDir, "slicing-*")
    if err != nil {
        return 0, fmt.Errorf("failed to create temp dir: %w", err)
    }
    defer os.RemoveAll(tempDir)

    // 5. Stream raw file from S3 and pipe to FFmpeg segmenter
    reader, err := pm.coord.objStore.GetObject(ctx, manifest.SourcePath)
    if err != nil {
        return 0, fmt.Errorf("failed to stream from S3: %w", err)
    }
    defer reader.Close()

    sliceCtx, sliceCancel := context.WithTimeout(ctx, 5*time.Minute)
    defer sliceCancel()

    cmd := exec.CommandContext(sliceCtx, "ffmpeg",
        "-i", "pipe:0",
        "-c", "copy",
        "-f", "segment",
        "-segment_format", "mp4",
        "-segment_time", "5",
        "-break_non_keyframes", "0",
        "-reset_timestamps", "1",
        filepath.Join(tempDir, "chunk_%03d.mp4"),
    )
    cmd.SysProcAttr = platformSysProcAttr()
    cmd.Stdin = reader

    if err := cmd.Run(); err != nil {
        return 0, fmt.Errorf("ffmpeg segmenting failed: %w", err)
    }

    // 6. Upload segments to S3
    files, err := os.ReadDir(tempDir)
    if err != nil {
        return 0, err
    }

    segmentCount := 0
    for _, file := range files {
        if file.IsDir() || !strings.HasSuffix(file.Name(), ".mp4") {
            continue
        }
        localPath := filepath.Join(tempDir, file.Name())
        s3Key := fmt.Sprintf("jobs/partition_%d/job_%s/raw/%s",
            pm.partitionID, jobID, file.Name())

        f, err := os.Open(localPath)
        if err != nil {
            return 0, fmt.Errorf("failed to open segment: %w", err)
        }
        fi, _ := f.Stat()
        if err := pm.coord.objStore.PutObject(ctx, s3Key, f, fi.Size()); err != nil {
            f.Close()
            return 0, fmt.Errorf("failed to upload segment %s: %w", file.Name(), err)
        }
        f.Close()
        segmentCount++
    }

    return segmentCount, nil
}

// validateInput runs ffprobe to verify the file is a valid video (ISD §3.2)
// and enforces the 12-hour maximum duration limit (DTDP §9.4). (I-14 fix)
func (pm *PartitionManager) validateInput(ctx context.Context, s3Key string) (bool, error) {
    // Download a small portion or stream for ffprobe validation
    reader, err := pm.coord.objStore.GetObject(ctx, s3Key)
    if err != nil {
        return false, err
    }
    defer reader.Close()

    cmd := exec.CommandContext(ctx, "ffprobe",
        "-v", "error",
        "-show_entries", "format=duration",
        "-of", "default=noprint_wrappers=1:nokey=1",
        "-i", "pipe:0",
    )
    cmd.Stdin = reader
    out, err := cmd.Output()
    if err != nil {
        return false, fmt.Errorf("ffprobe validation failed: %w", err)
    }

    durationSec, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
    if err == nil && durationSec > float64(MaxDurationHours*3600) {
        return false, fmt.Errorf("video duration %.0f seconds exceeds 12-hour limit", durationSec)
    }

    return true, nil
}

// checkMoovAtom checks if the moov atom is at the beginning of the file (ISD §3.1).
func (pm *PartitionManager) checkMoovAtom(ctx context.Context, s3Key string) (bool, error) {
    reader, err := pm.coord.objStore.GetObject(ctx, s3Key)
    if err != nil {
        return false, err
    }
    defer reader.Close()

    cmd := exec.CommandContext(ctx, "ffprobe",
        "-v", "error",
        "-show_entries", "format_tags=compatible_brands",
        "-i", "pipe:0",
    )
    cmd.Stdin = reader
    // If ffprobe fails to parse stream headers, moov is likely at the end
    if err := cmd.Run(); err != nil {
        return true, nil // needs faststart
    }
    return false, nil
}

// runFaststart moves the moov atom to the front of the file (ISD §3.1).
func (pm *PartitionManager) runFaststart(ctx context.Context, jobID, s3Key string) error {
    // Download, run qt-faststart, re-upload
    tempInput := filepath.Join(pm.coord.cfg.Worker.ScratchDir, fmt.Sprintf("%s_raw.mp4", jobID))
    tempOutput := filepath.Join(pm.coord.cfg.Worker.ScratchDir, fmt.Sprintf("%s_faststart.mp4", jobID))
    defer os.Remove(tempInput)
    defer os.Remove(tempOutput)

    if err := pm.coord.downloadFromS3(ctx, s3Key, tempInput); err != nil {
        return err
    }

    cmd := exec.CommandContext(ctx, "qt-faststart", tempInput, tempOutput)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("qt-faststart failed: %w", err)
    }

    // Re-upload the faststarted file
    f, err := os.Open(tempOutput)
    if err != nil {
        return err
    }
    defer f.Close()
    fi, _ := f.Stat()
    return pm.coord.objStore.PutObject(ctx, s3Key, f, fi.Size())
}

// markJobFailed sets job status to FAILED and notifies the client.
func (pm *PartitionManager) markJobFailed(ctx context.Context, jobID, reason string) {
    pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
        "state":        string(JobPhaseFailed),
        "last_updated": time.Now().Unix(),
        "error":        reason,
    })
    pm.coord.state.PublishProgress(ctx, jobID, ProgressUpdate{
        Phase: JobPhaseFailed,
        Error: reason,
    })
}

// loadManifest downloads and parses the job manifest from S3.
func (pm *PartitionManager) loadManifest(ctx context.Context, jobID string) (*JobManifest, error) {
    key := fmt.Sprintf("jobs/partition_%d/job_%s/job_manifest.json", pm.partitionID, jobID)
    reader, err := pm.coord.objStore.GetObject(ctx, key)
    if err != nil {
        return nil, err
    }
    defer reader.Close()

    var manifest JobManifest
    if err := json.NewDecoder(reader).Decode(&manifest); err != nil {
        return nil, err
    }
    return &manifest, nil
}
```

---

## 16. Security Layer (I-9 Fix)

Per DTDP §9, all tiers must enforce authentication, authorization, input validation, and rate limiting.

### 16.1 Rate Limiting Middleware

```go
package gateway

import (
    "context"
    "fmt"
    "net/http"
)

// RateLimiter enforces per-IP (100/min) and per-user (500/day) rate limits.
type RateLimiter struct {
    state         StateStore
    maxPerMinIP   int    // 100 uploads/min per IP
    maxPerDayUser int    // 500 uploads/day per User
}

func NewRateLimiter(state StateStore, maxPerMinIP, maxPerDayUser int) *RateLimiter {
    return &RateLimiter{state: state, maxPerMinIP: maxPerMinIP, maxPerDayUser: maxPerDayUser}
}

// AllowIP checks if the given IP has exceeded its per-minute rate limit.
func (rl *RateLimiter) AllowIP(ctx context.Context, clientIP string) (bool, error) {
    key := fmt.Sprintf("ratelimit:ip:%s", clientIP)
    count, err := rl.state.IncrRateLimit(ctx, key, 60) // 60s window
    if err != nil {
        return true, err // fail-open on Redis error
    }
    return count <= int64(rl.maxPerMinIP), nil
}

// AllowUser checks if the given user has exceeded their per-day rate limit.
func (rl *RateLimiter) AllowUser(ctx context.Context, userID string) (bool, error) {
    key := fmt.Sprintf("ratelimit:user:%s", userID)
    count, err := rl.state.IncrRateLimit(ctx, key, 86400) // 24h window
    if err != nil {
        return true, err // fail-open
    }
    return count <= int64(rl.maxPerDayUser), nil
}

// extractUserID extracts the subject/user identifier from a JWT token header.
func extractUserID(authHeader string) string {
    // Implementation placeholder: decodes "Bearer <token>" and returns claims.Subject
    return "user-id-from-jwt"
}

// Middleware returns an HTTP middleware that enforces rate limits.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        clientIP := extractClientIP(r)
        if allowed, _ := rl.AllowIP(r.Context(), clientIP); !allowed {
            http.Error(w, `{"error":"ip rate limit exceeded"}`, http.StatusTooManyRequests)
            // metrics.RateLimitRejects.Inc()
            return
        }
        
        // If request provides an Authorization header (JWT), check user limit
        if auth := r.Header.Get("Authorization"); auth != "" {
            userID := extractUserID(auth)
            if userID != "" {
                if allowed, _ := rl.AllowUser(r.Context(), userID); !allowed {
                    http.Error(w, `{"error":"user rate limit exceeded"}`, http.StatusTooManyRequests)
                    // metrics.RateLimitRejects.Inc()
                    return
                }
            }
        }
        next.ServeHTTP(w, r)
    })
}

// extractClientIP returns the client IP from X-Forwarded-For or RemoteAddr.
func extractClientIP(r *http.Request) string {
    // B-5 fix: removed redundant inner len check
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        parts := strings.SplitN(xff, ",", 2)
        return strings.TrimSpace(parts[0])
    }
    host, _, _ := net.SplitHostPort(r.RemoteAddr)
    return host
}
```

### 16.2 Input Validation (Upload Session)

```go
package gateway

import (
    "fmt"
    "net/http"
)

const (
    MaxUploadSizeBytes int64 = 50 * 1024 * 1024 * 1024 // 50 GB (B-6 fix: explicit int64)
    MaxDurationHours         = 12
)

// AllowedCodecs is the whitelist of accepted input codecs (DTDP §9.4).
var AllowedCodecs = map[string]bool{
    "h264": true, "h265": true, "hevc": true,
    "vp9": true, "av1": true, "prores": true,
    "mpeg2video": true, "mpeg4": true,
}

// ValidateUploadRequest checks file size and rejects oversized uploads.
func ValidateUploadRequest(req CreateSessionRequest) error {
    if req.FileSizeBytes <= 0 {
        return fmt.Errorf("file size must be positive")
    }
    if req.FileSizeBytes > MaxUploadSizeBytes {
        return fmt.Errorf("file size %d exceeds maximum %d bytes (50 GB)",
            req.FileSizeBytes, MaxUploadSizeBytes)
    }
    return nil
}
```

Integration in `CreateUploadSession()`:
```go
func (g *GatewayDaemon) CreateUploadSession(ctx context.Context, req CreateSessionRequest) (*UploadSession, error) {
    // Input validation (I-9)
    if err := ValidateUploadRequest(req); err != nil {
        return nil, fmt.Errorf("validation failed: %w", err)
    }
    // ... rest of existing flow
}
```

### 16.3 NATS mTLS Connection Configuration

```go
package infra

import (
    "crypto/tls"
    "crypto/x509"
    "os"

    "github.com/nats-io/nats.go"
)

// NewNATSConnection creates a NATS connection with mTLS client certificates.
// Per DTDP §9.2, each tier uses its own certificate for publish/subscribe scoping.
func NewNATSConnection(cfg NATSConfig) (*nats.Conn, error) {
    // Load client certificate and key
    cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
    if err != nil {
        return nil, fmt.Errorf("failed to load NATS client cert: %w", err)
    }

    // Load CA certificate
    caCert, err := os.ReadFile(cfg.TLSCA)
    if err != nil {
        return nil, fmt.Errorf("failed to load NATS CA cert: %w", err)
    }
    caCertPool := x509.NewCertPool()
    caCertPool.AppendCertsFromPEM(caCert)

    tlsConfig := &tls.Config{
        Certificates: []tls.Certificate{cert},
        RootCAs:      caCertPool,
        MinVersion:   tls.VersionTLS13,
    }

    nc, err := nats.Connect(
        strings.Join(cfg.URLs, ","),
        nats.Secure(tlsConfig),
        nats.MaxReconnects(-1),         // infinite reconnects
        nats.ReconnectWait(time.Second),
        nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
            log.Errorf("NATS error: %v", err)
        }),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to connect to NATS: %w", err)
    }
    return nc, nil
}
```

### 16.4 IAM Role Boundaries (Documentation)

Per DTDP §9.1, each tier operates under least-privilege S3 access:

| Tier | S3 Permissions | Restrictions |
|:---|:---|:---|
| **Gateway** | `CreateMultipartUpload`, `PutObject` (presigned), `AbortMultipartUpload` | No `DeleteObject`, no `ListBucket` |
| **Coordinator** | `GetObject`, `PutObject`, `ListObjectsV2`, `HeadObject`, `DeleteObject` (GC only) | Scoped to `jobs/partition_{owned_ids}/*` |
| **Worker** | `GetObject` (`raw/`), `PutObject` (`transcoded/`), `HeadObject` | No `DeleteObject`, no `ListBucket`, no write to `raw/` |

> **Implementation**: MinIO/S3 bucket policies enforce these restrictions via IAM policy conditions. Worker credentials cannot delete raw uploads or overwrite manifests. Only GC-enabled coordinators can delete objects.

---

## 17. S3/MinIO Event Bridge Configuration (I-12 Fix)

Per DTDP §3.4 and Infrastructure §2.1, `ObjectCreated` events from completed multipart uploads must reach the coordinator tier via NATS.

### Self-Hosted (MinIO): Native NATS Integration

MinIO natively publishes S3 events directly to NATS JetStream — no SQS bridge needed:

```bash
# Configure MinIO bucket notification to publish to NATS JetStream
mc admin config set myminio notify_nats:TRANSCODER \
    address="nats://nats-0:4222,nats://nats-1:4222,nats://nats-2:4222" \
    subject="s3-raw-uploads.job" \
    jetstream="on" \
    tls="on" \
    tls_cert="/certs/minio-nats-client.pem" \
    tls_key="/certs/minio-nats-client-key.pem"

# Enable event notifications for multipart upload completion only
mc event add myminio/transcoder arn:minio:sqs::TRANSCODER:nats \
    --event "s3:ObjectCreated:CompleteMultipartUpload" \
    --prefix "jobs/" \
    --suffix ".mp4"
```

### Cloud-Hosted (AWS S3): SQS → NATS Bridge

When using AWS S3 instead of self-hosted MinIO, a lightweight bridge daemon polls SQS and publishes to NATS:

```
S3 → SQS (EventBridge rule) → Bridge Daemon (Go) → NATS JetStream
```

The bridge daemon is a stateless Go service that:
1. Polls SQS in batches (`ReceiveMessage` with `WaitTimeSeconds=20`)
2. Filters events to only `CompleteMultipartUpload`
3. Publishes to NATS subject `s3-raw-uploads.job.{uuid}`
4. Deletes the SQS message after NATS ACK

### NATS Subject Mapping

Both approaches publish to the same NATS subject. NATS native subject mapping routes to partition-scoped coordinator topics:

```
s3-raw-uploads.job.*  →  job-uploads.partition.{{hash(1024, 1)}}.job.{{1}}
```

The `{{hash(1024, 1)}}` transform uses FNV-1a mod 1024 on the first wildcard token (job UUID), matching `models.PartitionOf()`.

---

## 18. Package Layout

```
transcoder/
├── cmd/
│   └── transcoder/
│       └── main.go               # Single binary entrypoint with --role flag
│
├── internal/
│   ├── models/
│   │   ├── job.go                # JobManifest, JobStatus, JobPhase
│   │   ├── task.go               # SegmentTask, BitIndex()
│   │   ├── partition.go          # PartitionOf() — shared hash function (D-1)
│   │   └── progress.go           # ProgressUpdate, UploadSession
│   │
│   ├── infra/
│   │   ├── interfaces.go         # ObjectStore, StateStore, MessageBus, Coordination
│   │   ├── redis_keys.go         # RedisKeys Hash Tag key builder (D-4)
│   │   ├── redis.go              # Redis Cluster implementation (with Hash Tags)
│   │   ├── minio.go              # MinIO/S3 implementation
│   │   ├── nats.go               # NATS JetStream implementation
│   │   ├── nats_mtls.go          # NATS mTLS connection with client certs (I-9)
│   │   └── etcd.go               # etcd implementation
│   │
│   ├── gateway/
│   │   ├── daemon.go             # GatewayDaemon.Run()
│   │   ├── handlers.go           # HTTP + WebSocket route handlers
│   │   ├── multiplexer.go        # ProgressMultiplexer (XREAD BLOCK fan-out)
│   │   ├── jwt.go                # Session token creation & validation
│   │   ├── ratelimit.go          # Per-IP Redis rate limiter (I-9)
│   │   └── validation.go         # Input validation: size, codec, duration (I-9)
│   │
│   ├── coordinator/
│   │   ├── daemon.go             # CoordinatorDaemon.Run()
│   │   ├── hashring.go           # Consistent Hash Ring (FNV-1a, 150 vnodes)
│   │   ├── partition.go          # PartitionManager (lifecycle per partition)
│   │   ├── slicer.go             # FFmpeg stream-copy slicing
│   │   ├── dispatcher.go         # NATS async task publishing
│   │   ├── manifest.go           # HLS + DASH manifest compilation
│   │   ├── gc.go                 # Job Garbage Collection daemon (I-4)
│   │   ├── dlq.go                # Dead Letter Queue monitor (I-6)
│   │   └── reconstruct.go        # 3-tier state reconstruction (Redis → S3)
│   │
│   ├── worker/
│   │   ├── daemon.go             # WorkerDaemon.Run() + graceful drain
│   │   ├── executor.go           # TaskExecutor (idempotency → transcode → pipeline)
│   │   ├── executor_linux.go     # Linux: Pdeathsig SysProcAttr (I-2)
│   │   ├── executor_darwin.go    # macOS: PID-polling parent watchdog (I-2)
│   │   ├── watchdog.go           # FFmpeg stall detection (LockOSThread)
│   │   ├── ffmpeg.go             # FFmpeg argument builder (HW accel presets)
│   │   ├── cgroup.go             # cgroups v2 resource fencing (I-1)
│   │   ├── circuit_breaker.go    # Redis failure circuit breaker
│   │   └── scratch.go            # Disk quota fencing + scratch GC
│   │
│   ├── metrics/
│   │   └── metrics.go            # Prometheus metric structs & registration (I-5)
│   │
│   ├── tracing/
│   │   └── tracing.go            # OpenTelemetry OTLP gRPC init (I-5)
│   │
│   └── config/
│       └── config.go             # YAML config loader (incl. MetricsConfig, TracingConfig)
│
├── go.mod
└── go.sum
```

---

## 19. Cross-Reference to Design Documents

| LLD Component | DTDP Section | HLD Section | ISD Section |
| :--- | :--- | :--- | :--- |
| `JobManifest` struct | §3.1 S3 Directory Layout | §2.2 Object Store | §1.1 Ingestion Flow |
| `SegmentTask.BitIndex()` | §3.2 Redis Key Layout | §3.3 Task Dispatch | — |
| `ProgressMultiplexer` | §4.5 Gateway Stream Multiplexing | §2.1 Gateway | — |
| `HashRing` | §5.1 Consistent Hash Ring | §2.3 Coordinator Sharding | — |
| `CoordinatorDaemon.selfFence()` | §5.1 Self-Fencing Timeline | §4.1 Chronological Timeline | — |
| `PartitionManager.sliceAndDispatch()` | §4.2 GOP-Aligned Slicing | §3.1 Slicing Flow | §2.1 Slicing Mechanics |
| `TaskExecutor.Execute()` | §4.3 Distributed Transcoding | §3.3 Task Execution | — |
| `TaskExecutor.runWatchdog()` | §5.2 Worker Watchdog | §4.2 Worker Self-Fencing | — |
| `CircuitBreaker` | §4.3 Circuit Breaker Note | §3.3 Idempotency | — |
| `ExecuteCompletionPipeline()` | §4.3 Completion Write Path | §3.3 Completion Path | — |
| `WorkerDaemon.Run()` drain | §5.6 Graceful Drain Protocol | §4.5.2 Drain Protocol | — |
| Redis Hash Tags `{uuid}` | §4.5 CROSSSLOT Note | §3.3 Hash Tag Note | — |
| NTP / monotonic clocks | §5.9 NTP Clock Drift | §4.2 NTP Safety | — |
| `models.PartitionOf()` (D-1) | §3.4 Partition Mapping | §2.3 Coordinator Sharding | §1.1 Ingestion |
| `RedisKeys` builder (D-4) | §4.3 Completion Pipeline | §3.3 Hash Tag Note | — |
| `CgroupFencer` (I-1) | §2.2 Resource Fencing | §3.3 Worker Constraints | — |
| Build tags (I-2) | §2.2 Pdeathsig / macOS | §4.2 Parent Death Signal | ISD §Note |
| Epoch fencing (I-3) | §5.1 Epoch Fencing | §4.1 Timeline | — |
| `JobGCDaemon` (I-4) | §5.8 Job GC | — | — |
| Metrics & Tracing (I-5) | §8.1-8.2 Observability | — | — |
| `platformSysProcAttr()` inline (R-1) | §2.2 Pdeathsig | §4.2 Parent Death Signal | ISD §Note |
| `models.PartitionOf()` inline (R-2) | §3.4 Partition Mapping | §2.3 Coordinator Sharding | §1.1 Ingestion |
| `probeDuration()` (D-5) | §4.3 Completion Write Path | §3.3 Completion Path | — |
| Manifest dedup guard (D-6) | §4.4 Manifest Compilation | §3.4 Manifest Stitching | — |
| `runDLQMonitor()` (I-6) | §3.4 Dead Letter Queue | §2.4 NATS DLQ | — |
| `StateStore.DeleteKeys()` (I-7) | §5.8 Job GC | — | — |
| `executeSlicing()` (I-8) | §4.2 GOP-Aligned Slicing | §3.1 Slicing Flow | §2.1 Slicing Mechanics |
| Security layer (I-9) | §9 Security | — | — |
| Upload ACK-before-semaphore (B-4) | §4.2 Slicing Concurrency | §3.1 Slicing Lock | §2.2 Concurrency Lock |
| `extractClientIP` cleanup (B-5) | §9.4 Rate Limiting | — | — |
| `MaxUploadSizeBytes int64` (B-6) | §9.4 Input Validation | — | — |
| Shard divisibility guard (D-7) | §3.4 Sharded Queues | §2.4 NATS Shards | — |
| DASH URL in COMPLETED (D-8) | §4.4 Manifest Compilation | §3.4 Manifest Stitching | — |
| `CoordinatorDaemon.Run()` (I-10) | §5.1 Coordinator Lifecycle | §2.3 Coordinator Tier | — |
| `CreateSessionRequest` (I-11) | §4.1 Upload Session | §3.1 Ingestion | §1.1 Ingestion |
| S3/MinIO event bridge (I-12) | §3.4 S3 Event Bridge | §1.1 Architecture | Infra §2.1 |
| `WorkerConfig.NodeID` (B-7) | §2.2 Single Binary | §2.5 Worker Tier | — |
| Multiplexer busy-loop fix (B-8) | §4.5 Gateway Multiplexing | §2.1 Gateway | — |
| Single-owner deregister (D-9) | §5.1 Coordinator Lifecycle | §4.1 Self-Fencing | — |
| `GatewayDaemon.Run()` (D-10) | §2.2 Single Binary | §2.1 Gateway | — |
| `reconstructState()` (I-13) | §5.1 State Reconstruction | §4.3 Shard Adoption | — |
| `RateLimiter` per-user (I-14) | §9.4 Rate Limiting | — | — |
| SSE fallback & Tus (I-14) | §4.5 Gateway SSE / §4.1 Tus | §2.1 Gateway | — |
| `validateInput` 12h limit (I-14) | §9.4 Input Validation | — | — |
| `runWatchdog` limits (I-14) | §5.3 Worker Watchdog | §4.2 Self-Fencing | — |

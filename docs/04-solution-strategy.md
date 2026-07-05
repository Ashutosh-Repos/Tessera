# 4. Solution Strategy

To satisfy the primary quality goals (Cloud Agnosticism, Extreme Scalability, and Idempotent Resiliency), the VOD Engine relies on two foundational architectural strategies: **The Pluggable Driver Model** and **The 3-Tier Compute Split**.

---

## 4.1 The Pluggable Driver Model (Vendor Independence)

To ensure the engine can run anywhere—from a single local developer laptop to an Oracle ARM mesh or an autoscaling AWS EKS cluster—the codebase strictly isolates infrastructure dependencies behind clean Go interfaces defined in [`internal/infra/`](../internal/infra/).

```
                 ┌────────────────────────────────────────────────────────┐
                 │                Core Compute Business Logic             │
                 │          (Gateway, Coordinator, Worker Daemons)        │
                 └───────────────────────────┬────────────────────────────┘
                                             │ Communicates strictly via Go Interfaces
     ┌───────────────────────────────────────┼───────────────────────────────────────┐
     ▼                                       ▼                                       ▼
┌─────────────────────────┐     ┌─────────────────────────┐     ┌─────────────────────────┐
│       StateStore        │     │       ObjectStore       │     │       MessageBus        │
│       (Interface)       │     │       (Interface)       │     │       (Interface)       │
└────────────┬────────────┘     └────────────┬────────────┘     └────────────┬────────────┘
             │                               │                               │
    ┌────────┴────────┐             ┌────────┴────────┐             ┌────────┴────────┐
    │   RedisStore    │             │    S3Client     │             │     NATSBus     │
    │  (go-redis/v9)  │             │ (MinIO/AWS S3)  │             │   (JetStream)   │
    └─────────────────┘             └─────────────────┘             └────────┬────────┘
                                                                             │ SQS Alternate
                                                                    ┌────────┴────────┐
                                                                    │     SQSBus      │
                                                                    │    (AWS SQS)    │
                                                                    └─────────────────┘
```

### Deep Analysis of Go Interface Contracts

#### 1. `StateStore` ([`store.go`](../internal/infra/store.go#L11))
Defines atomic state operations, caching, bitset progress tracking, and rate limiting:
```go
type StateStore interface {
    SetJobStatus(ctx context.Context, jobID string, status map[string]interface{}) error
    GetJobStatus(ctx context.Context, jobID string) (map[string]string, error)
    IncrJobCompleted(ctx context.Context, jobID string) (int64, error)
    SetBit(ctx context.Context, jobID string, bitIdx int) error
    BitCount(ctx context.Context, jobID string) (int64, error)
    TaskExists(ctx context.Context, jobID string, segment int, res string) (bool, error)
    SetTaskDone(ctx context.Context, jobID string, segment int, res string, ttl int) error
    SetSegmentDuration(ctx context.Context, jobID string, segRes string, duration string) error
    GetAllDurations(ctx context.Context, jobID string) (map[string]string, error)
    AddActiveJob(ctx context.Context, partitionID int, jobID string) error
    RemoveActiveJob(ctx context.Context, partitionID int, jobID string) error
    GetActiveJobs(ctx context.Context, partitionID int) ([]string, error)
    CacheManifest(ctx context.Context, jobID string, data []byte) error
    GetCachedManifest(ctx context.Context, jobID string) ([]byte, error)
    PublishProgress(ctx context.Context, jobID string, update models.ProgressUpdate) error
    ReadProgressStream(ctx context.Context, jobIDs []string, lastIDs []string, blockMs int) ([]StreamEntry, error)
    DeduplicateEvent(ctx context.Context, jobID string) (bool, error)
    IncrRateLimit(ctx context.Context, key string, windowSec int) (int64, error)
    ExecuteCompletionPipeline(ctx context.Context, p CompletionPipelineParams) error
    DeleteKeys(ctx context.Context, keys ...string) error
    ExpireJobKeys(ctx context.Context, jobID string, ttlSec int) error
    Ping(ctx context.Context) error
    ScanJobKeys(ctx context.Context) ([]string, error)
    RegisterWorker(ctx context.Context, workerID string, info map[string]interface{}, ttlSec int) error
    GetActiveWorkers(ctx context.Context) (map[string]map[string]string, error)
    Close() error
}
```

#### 2. `ObjectStore` ([`s3.go`](../internal/infra/s3.go#L18))
Defines object storage operations for uploading, presigning, copying, and deleting video media:
```go
type ObjectStore interface {
    CreateMultipartUpload(ctx context.Context, key string) (uploadID string, err error)
    GeneratePresignedPUT(ctx context.Context, key, uploadID string, partNum int, expiry time.Duration) (url string, err error)
    CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error
    AbortMultipartUpload(ctx context.Context, key, uploadID string) error
    PutObject(ctx context.Context, key string, body io.Reader, size int64) error
    GetObject(ctx context.Context, key string) (io.ReadCloser, error)
    HeadObject(ctx context.Context, key string) (ObjectMeta, error)
    CopyObject(ctx context.Context, srcKey, dstKey string) error
    DeleteObject(ctx context.Context, key string) error
    DeletePrefix(ctx context.Context, prefix string) error
    ListObjectsPrefix(ctx context.Context, prefix string) ([]string, error)
    Ping(ctx context.Context) error
}
```

#### 3. `MessageBus` ([`bus.go`](../internal/infra/bus.go#L9))
Defines event publishing, sharded worker queue pulling, partition event subscriptions, and DLQ streams:
```go
type MessageBus interface {
    PublishTaskAsync(ctx context.Context, shard int, priority string, payload []byte) error
    FlushPendingPublishes(ctx context.Context) error
    PublishEvent(ctx context.Context, subject string, payload []byte) error
    PullTasks(ctx context.Context, shard int, batchSize int) ([]TaskMessage, error)
    SubscribePartitionUploads(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error
    SubscribeCompletionEvents(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error
    SubscribeDLQ(ctx context.Context, handler func(msg TaskMessage)) error
    GetDLQDepth() (int64, error)
    InitEcosystem(shardCount int) error
    Ping(ctx context.Context) error
    Close() error
}
```

---

## 4.2 Infrastructure Driver Tradeoff & Decision Analysis

The engine supports alternate driver implementations configured via YAML settings.

| Driver Capability | NATS JetStream Driver (`nats.go`) | AWS SQS Driver (`sqs.go`) | Architectural Tradeoff Analysis |
| :--- | :--- | :--- | :--- |
| **Primary Target** | Local Docker, Oracle Cloud, Multi-Cloud Mesh | AWS Cloud Production | NATS requires zero cloud provider lock-in; SQS leverages AWS managed security IAM roles. |
| **Operational Memory Overhead** | ~15 MB RAM total | 0 MB (Cloud API) | NATS runs as a lightweight container in local dev; SQS charges per API request. |
| **Queue Sharding Topology** | JetStream Stream Subjects (`transcode-tasks.shard.>`) | SQS FIFO Queues (`transcode-tasks-shard-{id}.fifo`) | JetStream subject filtering enables high throughput; SQS FIFO ensures strict ordering per `MessageGroupId`. |
| **In-Flight Deadline Extension** | `msg.InProgress()` API call | `ChangeMessageVisibility` API call | Both drivers extend worker task deadlines while FFmpeg transcodes, preventing duplicate deliveries. |

---

## 4.3 3-Tier Compute Split & Binary Bootstrapping

The compute layer is split into three decoupled daemon roles. Each daemon compiles from the exact same Go entrypoint ([`cmd/transcoder/main.go`](../cmd/transcoder/main.go#L1)).

```
                               ┌─────────────────────────┐
                               │  cmd/transcoder/main.go │
                               │   (Cobra CLI Entrypoint)│
                               └────────────┬────────────┘
                                            │ Parses command flags (--config, --region)
                                            ▼
                               ┌─────────────────────────┐
                               │   initInfra Helper Fn   │
                               └────────────┬────────────┘
                                            │ Dynamically instantiates required drivers
         ┌──────────────────────────────────┼──────────────────────────────────┐
         ▼                                  ▼                                  ▼
┌──────────────────┐               ┌──────────────────┐               ┌──────────────────┐
│  server gateway  │               │server coordinator│               │  server worker   │
├──────────────────┤               ├──────────────────┤               ├──────────────────┤
│ • RateLimiter    │               │ • HashRing       │               │ • TaskExecutor   │
│ • ProgressMux    │               │ • SlicerSem      │               │ • CircuitBreaker │
│ • HTTP Server    │               │ • DLQ Monitor    │               │ • OS Watchdogs   │
│ • S3 Presigner   │               │ • GCDaemon       │               │ • Task Pullers   │
└──────────────────┘               └──────────────────┘               └──────────────────┘
```

1. **The Gateway (`server gateway`)**: The stateless edge. Boots `ProgressMultiplexer`, initializes rate limiters, signs S3 presigned PUT URLs, and serves HTTP APIs and SSE progress streams.
2. **The Coordinator (`server coordinator`)**: The stateful brain. Maintains the 1024-partition Etcd Hash Ring, executes faststart video slicing, enforces epoch fencing, monitors DLQ retries, and compiles HLS/DASH manifests.
3. **The Worker (`server worker`)**: The stateless muscle. Pulls tasks from NATS/SQS shards, enforces disk space checks and OS watchdogs, executes hardware-accelerated FFmpeg transcodes, and performs atomic S3 uploads.

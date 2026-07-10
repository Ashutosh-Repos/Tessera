# 2. Architecture Constraints

Tessera is bound by strict technical, hardware, operating system, and deployment constraints. These constraints dictate the low-level design of the Go codebase, memory allocation strategies, disk quota policies, and infrastructure integration boundaries.

---

## 2.1 System Parameters & Quantitative Bounds

The following table details the hard quantitative parameters configured in the unified configuration schema ([`config.go`](../internal/config/config.go#L13)):

```
 ┌──────────────────────────────────────────────────────────────────────────────────────────┐
 │                                System Quantitative Bounds                                │
 ├──────────────────────────┬────────────────────────┬──────────────────────────────────────┤
 │ Parameter Name           │ Default Configuration  │ Operational & Architectural Purpose  │
 ├──────────────────────────┼────────────────────────┼──────────────────────────────────────┤
 │ `MaxUploadSizeBytes`     │ 50 GB                  │ Hard ceiling on raw source uploads   │
 │ `PartitionCount`         │ 1024                   │ Etcd Hash Ring total partition space │
 │ `VirtualNodesPerCoord`   │ 150 VNodes             │ Consistent Hashing Ring balance      │
 │ `NATSShardCount`         │ 4 Shards               │ JetStream queue shard parallelism    │
 │ `MinDiskFreeGB`          │ 10 GB                  │ Worker pre-flight scratch disk check │
 │ `MaxTempFileSizeGB`      │ 3 GB                   │ OS Watchdog maximum temp file limit  │
 │ `MaxTaskDurationMin`     │ 5 Minutes              │ OS Watchdog FFmpeg execution timeout │
 │ `GracefulDrainSec`       │ 300 Seconds (5 Min)    │ Worker SIGTERM pod drain timeout     │
 │ `CircuitBreakerThresh`   │ 3 Failures in 5s       │ S3 Thundering Herd trip threshold    │
 └──────────────────────────┴────────────────────────┴──────────────────────────────────────┘
```

---

## 2.2 Deep Rationale for Technical Constraints

### 1. 50GB Upload Ceiling (`MaxUploadSizeBytes`)
*   **Rationale:** Allowing unrestricted video file sizes risks exhausting object storage budgets and causing integer overflow issues during byte calculations. 
*   **Enforcement:** The Gateway's [`ValidateUploadRequest`](../internal/gateway/handlers.go#L27) function validates the client's `FileSizeBytes` in the JSON payload of `POST /api/jobs/upload-session`. If `FileSizeBytes <= 0` or `FileSizeBytes > 53,687,091,200` (50GB), the request is rejected immediately with an HTTP 400 Bad Request error before any S3 multipart upload ID is requested.

### 2. 1024 Fixed Hash Partitioning (`PartitionCount`)
*   **Rationale:** To distribute active jobs evenly across Coordinators without maintaining a centralized database bottleneck, the system utilizes Consistent Hashing. 
*   **Enforcement:** The job UUID is mapped to a partition using `FNV-1a32(JobID) % 1024` ([`hashing.go`](../internal/models/hashing.go#L11)). The `PartitionCount` (1024) must be evenly divisible by the `NATSShardCount` (4); otherwise, the Coordinator daemon refuses to boot and logs a fatal configuration error ([`daemon.go`](../internal/coordinator/daemon.go#L51)).

### 3. Worker Local Scratch Disk & Watchdog Limits
*   **Rationale:** FFmpeg transcoding operations generate temporary `.ts` and `.mp4` chunks on the worker node's local filesystem (`/tmp/scratch`). If a corrupted video file causes FFmpeg to write infinite bytes or hang in an infinite loop, the worker node's host storage will fill up, crashing adjacent containers on the node.
*   **Enforcement:** 
    - **Disk Space Pre-Flight:** Before spawning an FFmpeg process, the executor calls `syscall.Statfs(scratchDir)` ([`executor.go:L44`](../internal/worker/executor.go#L44)). If available disk space is below `MinDiskFreeGB` (10GB), the task is NAKed and redelivered to another worker.
    - **Temp File Size Watchdog:** A dedicated goroutine monitors the output file size every `WatchdogIntervalSec` seconds ([`executor.go:L229`](../internal/worker/executor.go#L229)). If the file exceeds `MaxTempFileSizeGB` (3GB), it kills the entire FFmpeg process group with `syscall.Kill(-pid, SIGKILL)` — this uses process group signaling, not `pkill`.
    - **Execution Duration Watchdog:** If FFmpeg runs longer than `MaxTaskDurationMin` (5 minutes), the same watchdog kills the process group.

### 4. 100% Go Standard Library Preference & CGo Elimination
*   **Rationale:** Relying on C-bindings to libavcodec/libavformat via `cgo` introduces memory leaks, segmentation faults that crash the Go runtime, cross-compilation headaches, and strict C-library dependency locks.
*   **Enforcement:** The codebase uses standard Go features (`net/http`, `context`, `sync`, `os/exec`) requiring Go 1.24+. Transcoding relies strictly on external invocation of the `ffmpeg` CLI binary. This allows operators to swap FFmpeg versions, inject custom build flags, or enable proprietary GPU hardware acceleration drivers (`nvenc`, `vaapi`, `videotoolbox`) independently of the Go application binary.

### 5. 1MB Faststart Moov Atom Inspection Buffer
*   **Rationale:** Large high-bitrate / 4K MP4 videos can feature extensive `moov` atom sample tables exceeding 64KB. Reading 1MB from the S3 stream prefix ensures faststart moov structures are reliably identified before slicing without resorting to unnecessary full-file S3 downloads.

### 5. Shared-Nothing State Isolation
*   **Rationale:** In a cloud-native autoscaling environment, compute nodes (Gateways, Coordinators, Workers) must be completely ephemeral. Nodes can be terminated at any moment by Kubernetes HPA, KEDA, or spot-instance evictions.
*   **Enforcement:** No node is permitted to store state locally on disk or in local RAM across requests. All state is externalized to Redis Cluster (job status, progress bitmaps, streams), Etcd (coordinator membership, partition leases, slicing mutexes), and MinIO/S3 (raw video files, segment chunks, playlists, manifests).

---

## 2.3 Operating System & Cross-Platform Constraints

*   **Linux / Darwin Architecture Support:** The codebase includes platform-specific build tags (`process_linux.go` vs `process_darwin.go` and `executor_linux.go` vs `executor_darwin.go`) to handle differences in process signaling, process group killing, and disk stat system calls between Linux server environments and macOS developer workstations.
*   **ARM64 Native Compilation:** The Go codebase and Alpine Docker container image must build natively for Linux `aarch64`. This enables running the entire stack on Oracle Cloud Ampere A1 Compute instances (4 ARM OCPUs, 24GB RAM Always Free Tier) with zero emulation overhead.

# 🐛 System Issues & Resource Efficiency Findings

This directory contains documented technical findings, performance bottlenecks, and resource efficiency issues discovered during deep semantic codebase audits of the Tessera VOD engine.

## 📋 Index of Documented Findings

| Issue ID | Category | Impact Level | Description & Target Component | Status |
| :--- | :--- | :---: | :--- | :---: |
| **[ISSUE-001](./ISSUE-001-s3-triple-call-overhead.md)** | Storage / S3 I/O | **Medium-High** | 3× S3 API operations (`Put` + `Copy` + `Delete`) per segment in Worker (`internal/worker/executor.go`) | **Resolved (PR #8)** |
| **[ISSUE-002](./ISSUE-002-ffprobe-process-forking.md)** | Compute / CPU | **Medium-High** | Spawning external `ffprobe` process for duration probing after every segment (`internal/worker/executor.go`) | **Resolved (PR #2)** |
| **[ISSUE-003](./ISSUE-003-coordinator-disk-slice-staging.md)** | Disk I/O | **Medium-High** | Coordinator stage-writes raw segment files to local SSD before uploading (`internal/coordinator/slicer.go`) | **Resolved (PR #14)** |
| **[ISSUE-004](./ISSUE-004-nats-worker-polling-backoff.md)** | Latency / Queue | **Low-Medium** | `taskPuller` uses fixed 100ms sleep backoff when task channel is empty (`internal/worker/daemon.go`) | Open |
| **[ISSUE-005](./ISSUE-005-slicing-pipeline-serialization.md)** | Ingestion / Pipeline | **High** | Slicing phase blocks task dispatch until 100% of slices are generated & uploaded (`internal/coordinator/slicer.go`) | **Resolved (PR #14)** |
| **[ISSUE-006](./ISSUE-006-sequential-slice-uploads.md)** | Storage / Network | **Medium** | `uploadSlices` uploads raw `.mp4` segment chunks sequentially in single-threaded loop (`internal/coordinator/slicer.go`) | **Resolved (PR #13)** |

---

## ⚡ Summary Scorecard: Hardware Efficiency

* **Overall Hardware Efficiency Score**: **9.0 / 10** (Up from 8.5 after ISSUE-003 disk cleanup + ISSUE-005 pipelined dispatch).
* **Primary Strengths**: Zero-bandwidth gateway edge, in-memory faststart slicing, single-RTT Redis completion pipeline, progress multiplexer (1 Redis connection / node), cgroups v2 process isolation, manifest-only cross-region replication, pipelined chunk upload→dispatch→cleanup.
* **Target Optimization Potential**: Resolving the remaining open issue (`ISSUE-004`) is estimated to further reduce idle worker polling overhead by **5%–10%**.

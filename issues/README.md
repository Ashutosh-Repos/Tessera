# 🐛 System Issues & Resource Efficiency Findings

This directory contains documented technical findings, performance bottlenecks, and resource efficiency issues discovered during deep semantic codebase audits of the Tessera VOD engine.

## 📋 Index of Documented Findings

| Issue ID | Category | Impact Level | Description & Target Component | Status |
| :--- | :--- | :---: | :--- | :---: |
| **[ISSUE-001](./ISSUE-001-s3-triple-call-overhead.md)** | Storage / S3 I/O | **Medium-High** | 3× S3 API operations (`Put` + `Copy` + `Delete`) per segment in Worker (`internal/worker/executor.go`) | Open |
| **[ISSUE-002](./ISSUE-002-ffprobe-process-forking.md)** | Compute / CPU | **Medium** | Spawning external `ffprobe` process for duration probing after every segment (`internal/worker/executor.go`) | Open |
| **[ISSUE-003](./ISSUE-003-coordinator-disk-slice-staging.md)** | Disk I/O | **Medium** | Coordinator stage-writes raw segment files to local disk before uploading (`internal/coordinator/slicer.go`) | Open |
| **[ISSUE-004](./ISSUE-004-nats-worker-polling-backoff.md)** | Latency / Queue | **Low-Medium** | `taskPuller` uses 100ms sleep backoff when task channel is empty (`internal/worker/daemon.go`) | Open |

---

## ⚡ Summary Scorecard: Hardware Efficiency

* **Overall Hardware Efficiency Score**: **8.8 / 10**
* **Primary Strengths**: Zero-bandwidth gateway edge, in-memory faststart slicing, single-RTT Redis completion pipeline, progress multiplexer (1 Redis connection / node), cgroups v2 process isolation, manifest-only cross-region replication.
* **Target Optimization Potential**: Resolving the 4 issues above is estimated to increase worker compute density and reduce I/O latency by **15%–25%**.

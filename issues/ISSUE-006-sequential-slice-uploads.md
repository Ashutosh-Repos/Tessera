# ISSUE-006: Single-Threaded Sequential Slice Upload Loop in Coordinator

## 📌 Metadata
* **ID**: ISSUE-006
* **Component**: Coordinator Upload Engine
* **File**: [`internal/coordinator/slicer.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/coordinator/slicer.go#L262-L302)
* **Category**: Storage I/O & Network Concurrency
* **Impact**: Medium (Blocking Ingestion Phase Delay)

---

## 🔍 Description

In [`internal/coordinator/slicer.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/coordinator/slicer.go#L262-L302), after FFmpeg finishes slicing raw video into temporary chunk files on disk, `uploadSlices` iterates through the directory and uploads each slice file to S3 sequentially in a single-threaded `for` loop:

```go
// internal/coordinator/slicer.go
func (pm *PartitionManager) uploadSlices(ctx context.Context, jobID string, tempDir string) (int, error) {
    files, err := os.ReadDir(tempDir)
    // ...
    for _, file := range files {
        // ...
        // Sequential blocking S3 PutObject call per segment chunk
        err = pm.coord.objStore.PutObject(ctx, destKey, f, stat.Size())
        f.Close()
        if err != nil { return 0, err }
        segmentCount++
    }
    return segmentCount, nil
}
```

---

## 💥 Resource Impact

* **High Network Latency Penalty**: Each `PutObject` call requires a round-trip to S3/MinIO over HTTP/HTTPS (~15ms–40ms per call).
* For a video generating 500 segment chunks, uploading them sequentially takes:
  $$\text{Latency} = 500 \times 30\text{ms} = 15.0\text{ seconds of pure serial blocking time}$$
* Network bandwidth between Coordinator and S3 remains under-utilized because only 1 HTTP upload connection is active at any given time.

---

## ⚠️ "Strings Attached" (Risks & Trade-Offs)

1. **S3 Connection & Socket Exhaustion**: Launching unbounded goroutines for thousands of chunks could exhaust local TCP sockets or HTTP connection pools.
2. **Error Handling & Cancellation Context**: If one slice upload fails, all concurrent in-flight uploads for that job must be cancelled promptly.
3. **Mitigation**: Use a bounded worker pool (e.g. 8–16 concurrent upload workers) using `golang.org/x/sync/errgroup` with `SetLimit(10)`.

---

## 🛠️ Proposed Solution

Parallelize slice uploads using a bounded concurrency pool:

```go
g, gCtx := errgroup.WithContext(ctx)
g.SetLimit(10) // Limit to 10 concurrent S3 uploads

for _, file := range files {
    file := file
    g.Go(func() error {
        // Perform S3 PutObject in parallel
        return pm.coord.objStore.PutObject(gCtx, destKey, f, stat.Size())
    })
}
if err := g.Wait(); err != nil {
    return 0, err
}
```

---

## 📊 Expected Resource Gain

* **80%–90% reduction** in total slice upload phase latency.
* Maximizes network interface throughput between Coordinator and S3.

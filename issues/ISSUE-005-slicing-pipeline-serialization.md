# ISSUE-005: Serialized Slicing-to-Transcode Task Dispatch Pipeline

## 📌 Metadata
* **ID**: ISSUE-005
* **Component**: Coordinator Slicer Engine
* **File**: [`internal/coordinator/slicer.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/coordinator/slicer.go#L38-L81)
* **Category**: Ingestion Pipeline & Worker Utilization
* **Impact**: High (Worker Idle Time During Ingestion Phase)

---

## 🔍 Description

In [`internal/coordinator/slicer.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/coordinator/slicer.go#L38-L81), `sliceAndDispatch` operates in a strictly synchronous two-phase pipeline:
1. First, `executeSlicing()` runs FFmpeg to slice the raw input video into 5-second chunks (`chunk_000.mp4`, `chunk_001.mp4`, ...) and uploads **all** generated slices to S3.
2. Only after 100% of slices are uploaded and `executeSlicing()` returns, does the coordinator loop over `segmentCount` and publish transcode tasks to NATS JetStream.

```go
// internal/coordinator/slicer.go
// Phase 1: Wait for ALL slices to be generated and uploaded to S3
segmentCount, err := pm.executeSlicing(ctx, jobID)
if err != nil { ... }

// Phase 2: Dispatch tasks to NATS JetStream ONLY after Phase 1 finishes
for seg := 0; seg < segmentCount; seg++ {
    for _, res := range models.AllResolutions {
        // Publish task to NATS
    }
}
```

---

## 💥 Resource Impact

* **Worker Starvation / Idle Time**: For long high-resolution videos (e.g. a 2-hour 4K video generating 1,440 segments), slicing and uploading raw chunks can take 2–3 minutes. During this entire period, worker nodes sit completely idle waiting for tasks in NATS.
* **Elevated E2E Latency**: Overall VOD processing turnaround time (End-to-End latency) is artificially extended by the lack of pipeline overlap between ingestion and transcoding.

---

## ⚠️ "Strings Attached" (Risks & Trade-Offs)

1. **Unknown Total Segment Count**: NATS tasks published early during slicing will not carry `TotalTasks` until slicing finishes. However, worker task execution only needs individual `SegmentIdx` and `RawChunkKey`.
2. **Job Status & Bitmap Pre-allocation**: Redis completion progress bitmaps and `totalTasks` counters in job status hashes currently expect `totalTasks` to be set when transitioning to `TRANSCODING` state.
3. **Mitigation**: Update job manifest and Redis `totalTasks` count once FFmpeg completes, while allowing task dispatch to stream concurrently as raw `.mp4` chunks are committed to S3.

---

## 🛠️ Proposed Solution

Implement **Streamed Pipelined Dispatch**:
1. As FFmpeg outputs each `chunk_XXX.mp4` slice to S3, immediately dispatch the corresponding transcode tasks for all resolutions (`1080p`, `720p`, `480p`) to NATS JetStream.
2. Workers begin transcoding `chunk_000.mp4` and `chunk_001.mp4` while FFmpeg is still slicing `chunk_050.mp4`.

---

## 📊 Expected Resource Gain

* **Eliminates worker idle gap** at the start of video ingestion.
* Reduces overall End-to-End video processing turnaround time by **20%–40%**.

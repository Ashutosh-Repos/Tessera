# ISSUE-001: 3× S3 API Operation Overhead on Segment Completion

## 📌 Metadata
* **ID**: ISSUE-001
* **Component**: Worker Transcode Engine
* **File**: [`internal/worker/executor.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/worker/executor.go#L133-L152)
* **Category**: Storage & Network I/O
* **Impact**: Medium-High (Amplified by total segment count)

---

## 🔍 Description

When a worker completes transcoding a video segment, it currently executes 3 sequential S3 storage operations to save the output:
1. `PutObject` to a temporary key (`segment_001_1080p.ts.worker-id.tmp`)
2. `CopyObject` from the temporary key to the canonical path (`segment_001_1080p.ts`)
3. `DeleteObject` of the temporary key

### Code Snapshot
```go
// internal/worker/executor.go
tempOutputKey := fmt.Sprintf("%s.%s.tmp", task.OutputKey, te.cfg.NodeID)
if err := te.objStore.PutObject(ctx, tempOutputKey, f, fi.Size()); err != nil {
    return fmt.Errorf("failed to upload transcode output: %w", err)
}

// Atomic rename to canonical path
te.objStore.CopyObject(ctx, tempOutputKey, task.OutputKey)
te.objStore.DeleteObject(ctx, tempOutputKey)
```

---

## 💥 Resource Impact

For a standard 10-minute video producing 120 segments across 3 resolutions (360 total tasks):
* **S3 Operations executed**: 360 × 3 = **1,080 S3 API calls** instead of 360.
* **Latency Overhead**: Extra ~15ms–40ms per task waiting for S3 `CopyObject` + `DeleteObject` HTTP round-trips.
* **Cost Impact**: Increased S3/MinIO API request cost ($0.005 per 1,000 PUT/COPY calls on AWS S3).

---

## ⚠️ "Strings Attached" (Risks & Trade-Offs)

1. **Risk of Incomplete Object Exposure (Non-Atomic Uploads)**: If a worker node crashes mid-stream during `PutObject(task.OutputKey)`, S3 or MinIO might leave a zero-byte or partially written object under the canonical key.
2. **False-Positive Idempotency Trigger**: If a retrying worker relies on `HeadObject(task.OutputKey)` as a fallback idempotency check when Redis is down, an incomplete file could cause `HeadObject` to return `Exists == true`, marking a corrupted segment as completed.
3. **Mitigation / Safe Implementation**:
   - Perform local file size check BEFORE uploading.
   - S3 `PutObject` is atomic for single-part HTTP requests once payload is fully uploaded.
   - In `checkIdempotency()`, verify `HeadObject.Size > 0` rather than just `Exists == true`.

---

## 🛠️ Proposed Solution

Directly upload to `task.OutputKey` after validating local file non-zero size.

### Recommended Fix:
```go
// Upload directly to canonical S3 destination key after size verification
if fi.Size() == 0 {
    return fmt.Errorf("transcoded output file %s is 0 bytes", localOutput)
}
if err := te.objStore.PutObject(ctx, task.OutputKey, f, fi.Size()); err != nil {
    return fmt.Errorf("failed to upload transcode output to storage: %w", err)
}
```

---

## 📊 Expected Resource Gain
* **66% reduction** in S3 API calls during segment output upload phase.
* Eliminates ~20ms latency per completed segment.

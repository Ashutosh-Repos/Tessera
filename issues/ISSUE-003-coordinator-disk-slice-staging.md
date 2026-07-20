# ISSUE-003: Coordinator Segment Disk Staging During Ingestion

## 📌 Metadata
* **ID**: ISSUE-003
* **Component**: Coordinator Slicer Engine
* **File**: [`internal/coordinator/slicer.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/coordinator/slicer.go#L262-L295)
* **Category**: Disk I/O & Storage Bandwidth
* **Impact**: Medium (Local Disk Write/Read Thrashing on Coordinator Nodes)

---

## 🔍 Description

During video ingestion, the coordinator slices the raw MP4 video into 5-second chunk files (`chunk_000.mp4`, `chunk_001.mp4`, ...).
Currently, FFmpeg writes all slice files into a local temporary directory (`tempDir`) on local disk. Once FFmpeg completes, `uploadSlices()` reads each chunk back from disk and uploads it to S3.

### Code Snapshot
```go
// internal/coordinator/slicer.go
func (pm *PartitionManager) uploadSlices(ctx context.Context, jobID string, tempDir string) (int, error) {
    files, err := os.ReadDir(tempDir)
    // ...
    for _, file := range files {
        filePath := filepath.Join(tempDir, file.Name())
        f, err := os.Open(filePath)
        // Read from local disk and upload to S3 sequentially
        err = pm.coord.objStore.PutObject(ctx, destKey, f, stat.Size())
        f.Close()
    }
}
```

---

## 💥 Resource Impact

* **Double Disk I/O**: High-bitrate 4K input videos write tens of gigabytes of raw chunk files to coordinator local SSDs, only to immediately read them back for S3 upload.
* **Disk Space Contention**: Under high concurrent slicing volume, local disk space can fill rapidly, risking IOPS saturation.

---

## ⚠️ "Strings Attached" (Risks & Trade-Offs)

1. **Unknown Total Segment Count During Streaming**: If slices are uploaded asynchronously as FFmpeg emits them, the coordinator does not know the final `segmentCount` until FFmpeg finishes slicing.
2. **Asset Generation Dependency (Thumbnails & Sprites)**: Asset generation (`generateAssets`) relies on extracting preview images from segment chunks. If chunks are deleted immediately after streaming upload, asset generation would fail unless sample frames are buffered or extracted concurrently during slicing.
3. **Mitigation / Safe Implementation**:
   - Keep a lightweight sample directory for keyframe thumbnails while streaming `.mp4` segment chunks directly to S3.
   - Wait for FFmpeg process exit code before marking `segmentCount` in the S3 job manifest.

---

## 🛠️ Proposed Solution

Pipe segment creation in parallel directly to S3 as FFmpeg outputs them:
1. Use an `in-memory buffer pipe` or a light file watcher during FFmpeg execution to stream finished chunks to S3 asynchronously.
2. Parallelize slice uploads across worker pools rather than sequential single-threaded coordinator uploads.

---

## 📊 Expected Resource Gain
* Reduces coordinator local disk IOPS by **50%+**.
* Faster initial slicing-to-transcoding transition time.

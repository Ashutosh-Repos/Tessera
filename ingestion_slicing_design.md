# Detailed Design: Ingestion & Slicing Engine

> **⚠️ NOTICE**: This document provides the low-level design for ingestion and slicing. The authoritative system-wide design is in **distributed_transcoder_design_plan.md v3.1**. In case of conflict, that document takes precedence.

---

## 1. High-Level Design (HLD) & Data Flow

To prevent application memory and disk saturation, all media data uploads bypass the application server control plane and flow directly to storage.

```
┌──────────────┐          1. Request Multipart URLs         ┌──────────────┐
│  Client App  ├───────────────────────────────────────────>│  Ingest API  │
│  (Browser)   │<───────────────────────────────────────────┤   Gateway    │
└──────┬───────┘           2. Return Presigned URLs         └──────────────┘
       │                     + wss:// progress channel
       │ 3. Parallel Upload (HTTP PUT parts)
       ▼
┌──────────────────────────────┐
│   Distributed Object Store   │
│     (S3 / MinIO Buckets)     ├────────────────────────────┐
└──────────────────────────────┘                            │ 4. ObjectCreated
                                                            ▼ (S3 → SQS → NATS)
                                                    ┌──────────────┐
                                                    │  Coordinator │
                                                    │  (Slicer)    │
                                                    └──────────────┘
```

### 1.1 Ingestion Flow
1.  **Initiate Session**: Client requests an upload session via `POST /api/jobs/upload-session` specifying the total video file size.
2.  **Generate Presigned URLs**: Ingest API Gateway initiates S3 Multipart Upload and returns a **long-lived JWT session token (24h expiry)**. To prevent presigned URLs from expiring during massive 4K video uploads over slow connections, the client uses the JWT to fetch small batches of presigned PUT URLs just-in-time as the upload progresses.
3.  **Upload**: Client browser performs concurrent HTTP PUT requests directly to S3.
4.  **Complete Ingestion**: S3 generates an `ObjectCreated` event upon multipart assembly, which routes through **S3 → SQS → NATS bridge** to the `job-uploads` queue.

---

## 2. Low-Level Design (LLD) & Slicing Mechanics

### 2.1 Coordinator-Owned Slicing
Slicing is performed by the **coordinator** that owns the job's partition (not a general worker node). This ensures the coordinator has full control over segment indexing and can immediately proceed to task dispatch.

*   **Low-RAM Stream Slicing (`-c copy`)**: The coordinator streams the raw file from S3 and pipes it directly to FFmpeg segmenter in stream-copy mode.
*   **Segment Copy Optimization**: Because the coordinator copies packets without decoding (`-c copy`), the CPU load is negligible, and RAM utilization remains bounded below **50MB**, eliminating OOM risk.
*   **GOP-Aligned Cuts**: FFmpeg is configured with `-break_non_keyframes 0` to ensure segments are cut at **I-frame (keyframe) boundaries**, making each chunk independently decodable.
*   **Segments Output**: FFmpeg writes segments (`chunk_001.mp4`, `chunk_002.mp4`) locally to SSD and uploads them to `/jobs/partition_{id}/job_{uuid}/raw/` folder in the bucket.
*   **Concurrency Limit**: Each coordinator limits concurrent slicing to **50 parallel jobs** (semaphore) to prevent slicing backlogs during peak upload bursts.

### 2.2 Slicing Concurrency Lock
*   **The Issue**: If two coordinators trigger the same slicing execution due to a network partition or hash ring rebalance, they could concurrently stream-slice the same raw file, overwriting S3 slices and corrupting segment indexing.
*   **The Lock**: We enforce a **strict single-active lock** in `etcd` under the path `/locks/slicing/{job_uuid}`. The coordinator must acquire this exclusive lock with a 10-second TTL (renewed every 3 seconds via keep-alive) before triggering the slicing process. If lock acquisition fails or is lost mid-slice, the slicing routine aborts immediately, preventing overlapping writes to S3.

---

## 3. Real-World Bug Mitigations & Integrity Checks

### 3.1 Non-Faststart Validation & Metadata Recovery (`moov` atom)
*   **The Bug**: If the uploaded video has its index metadata (`moov` atom) at the end of the file (default for raw cameras), FFmpeg cannot parse stream parameters on-the-fly without downloading the whole file first, causing stream-slicing to fail.
*   **The Fix**:
    1.  The coordinator performs a lightweight `ffprobe` check on the stream headers. If the index atom is missing from the beginning, it aborts stream-slicing.
    2.  It dispatches a `FaststartTask` to run `qt-faststart` (which moves the `moov` index atom to the front), then re-attempts slicing.

### 3.2 Direct Upload Verification (Input Sanitation)
*   **The Bug**: A client can upload a corrupted file or malicious payload renamed to `.mp4`. When the coordinator processes the file, it will crash the parser.
*   **The Fix**: Before running the segmenting process, the coordinator runs `ffprobe -v error -show_format` on the file descriptor. If validation fails, the coordinator deletes the temp data and immediately marks the job as `FAILED` via Redis (`HSET job:{uuid}:status state FAILED`) and notifies the client via WebSocket.

```go
package slicer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type SlicingWorker struct {
	ID string
}

func (w *SlicingWorker) ExecuteSlice(ctx context.Context, jobID string, rawFileURL string) error {
	// 1. Create temporary directory for segments
	tempDir, err := os.MkdirTemp("", "slicing-job-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// 2. Open S3 stream
	respStream, err := getS3Stream(rawFileURL)
	if err != nil {
		return err
	}
	defer respStream.Close()

	// 3. Configure streaming FFmpeg slice subprocess with GOP-aligned cuts
	sliceCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(sliceCtx, "ffmpeg",
		"-i", "pipe:0",
		"-c", "copy",
		"-f", "segment",
		"-segment_format", "mp4",
		"-segment_time", "5",
		"-break_non_keyframes", "0",
		"-reset_timestamps", "1",
		fmt.Sprintf("%s/chunk_%%03d.mp4", tempDir),
	)

	// Ensure FFmpeg dies if coordinator process crashes
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:  true,
		Pdeathsig: syscall.SIGKILL,
	}

	// Pipe incoming S3 network stream directly into FFmpeg stdin
	cmd.Stdin = respStream

	// 4. Run FFmpeg (-c copy maintains <50MB RAM profile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("physical stream copy segmenting failed: %w", err)
	}

	// 5. Read output segments directory and upload to S3
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		filePath := fmt.Sprintf("%s/%s", tempDir, file.Name())
		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		destPath := fmt.Sprintf("/jobs/%s/raw/%s", jobID, file.Name())
		if err := w.uploadToS3(destPath, f); err != nil {
			f.Close()
			return fmt.Errorf("failed to upload segment %s: %w", file.Name(), err)
		}
		f.Close()
	}

	return nil
}

func getS3Stream(url string) (io.ReadCloser, error) { return nil, nil }
func (w *SlicingWorker) uploadToS3(dest string, data io.Reader) error { return nil }
```

> **Note (macOS)**: `Pdeathsig` is Linux-only. On macOS (Apple Silicon VPU workers), a PID-polling watchdog goroutine checks `os.Getppid()` every second. If the parent PID changes to 1 (launchd), FFmpeg is killed. Use build tags to conditionally set `SysProcAttr`.

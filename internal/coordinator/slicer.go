package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/distributed-transcoder/internal/models"
)

func (pm *PartitionManager) sliceJob(ctx context.Context, jobID string) {
	pm.sliceAndDispatch(ctx, jobID)
}

func (pm *PartitionManager) sliceAndDispatch(ctx context.Context, jobID string) {
	// 1. Acquire etcd slicing lock
	acquired, _ := pm.coord.coord.AcquireSlicingLock(ctx, jobID, pm.coord.nodeID,
		pm.coord.cfg.Coordinator.SlicingLockTTLSec)
	if !acquired {
		log.Printf("Job %s: slicing lock already held by another coordinator", jobID)
		return
	}
	defer pm.coord.coord.ReleaseSlicingLock(ctx, jobID)

	// 2. Update phase to SLICING
	pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
		"state": string(models.JobPhaseSlicing), "last_updated": time.Now().Unix(),
	})
	pm.coord.state.PublishProgress(ctx, jobID, models.ProgressUpdate{Phase: models.JobPhaseSlicing})

	// 3. Execute stream-slicing via ffmpeg
	segmentCount, err := pm.executeSlicing(ctx, jobID)
	if err != nil {
		log.Printf("Job %s: slicing failed: %v", jobID, err)
		pm.markJobFailed(ctx, jobID, err.Error())
		return
	}

	// 4. Update manifest with segment count
	totalTasks := segmentCount * len(models.AllResolutions)
	pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
		"state": string(models.JobPhaseTranscoding), "total": totalTasks,
		"last_updated": time.Now().Unix(),
	})

	// 5. Dispatch all tasks via NATS JetStream async publish
	for seg := 0; seg < segmentCount; seg++ {
		for _, res := range models.AllResolutions {
			task := models.SegmentTask{
				JobID:       jobID,
				PartitionID: pm.partitionID,
				OwnerEpoch:  pm.coord.currentEpoch,
				SegmentIdx:  seg,
				Resolution:  res,
				RawChunkKey: fmt.Sprintf("jobs/partition_%d/job_%s/raw/chunk_%03d.mp4", pm.partitionID, jobID, seg),
				OutputKey:   fmt.Sprintf("jobs/partition_%d/job_%s/transcoded/segment_%03d_%s.ts", pm.partitionID, jobID, seg, res),
				HWAccel:     pm.coord.cfg.Worker.HWAccel,
				Priority:    "normal",
			}
			payload, _ := json.Marshal(task)
			shard := pm.partitionID / (pm.coord.cfg.Coordinator.PartitionCount / pm.coord.cfg.Coordinator.NATSShardCount)
			pm.coord.bus.PublishTaskAsync(ctx, shard, task.Priority, payload)
		}
	}
	// Flush all async publishes (blocks until NATS confirms all)
	pm.coord.bus.FlushPendingPublishes(ctx)
	pm.coord.state.PublishProgress(ctx, jobID, models.ProgressUpdate{Phase: models.JobPhaseTranscoding, Total: totalTasks})
}

func (pm *PartitionManager) executeSlicing(ctx context.Context, jobID string) (int, error) {
	// 1. Load job manifest to find SourcePath
	manifest, err := pm.loadManifest(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("failed to load manifest: %w", err)
	}

	// 2. Create temporary directory for local processing
	tempDir, err := os.MkdirTemp("", "slicing-job-"+jobID+"-*")
	if err != nil {
		return 0, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// 3. Open S3 stream to check faststart
	stream, err := pm.coord.objStore.GetObject(ctx, manifest.SourcePath)
	if err != nil {
		return 0, fmt.Errorf("failed to get source video from S3: %w", err)
	}
	defer stream.Close()

	// Read first 64KB for faststart check
	buf := make([]byte, 65536)
	n, err := io.ReadFull(stream, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return 0, fmt.Errorf("failed to read stream prefix: %w", err)
	}
	prefix := buf[:n]

	isFast := isFaststart(prefix)

	var segmentCount int
	if isFast {
		log.Printf("Job %s: faststart moov atom detected. Stream-slicing from S3.", jobID)
		segmentCount, err = pm.streamSlice(ctx, jobID, prefix, stream, tempDir)
	} else {
		log.Printf("Job %s: non-faststart/fragmented video. Downloading to run qt-faststart equivalent.", jobID)
		segmentCount, err = pm.downloadAndSlice(ctx, jobID, prefix, stream, tempDir, manifest.SourcePath)
	}

	if err != nil {
		return 0, err
	}

	// 4. Update manifest in S3 with segment count
	manifest.SegmentCount = segmentCount
	manifest.TotalTasks = segmentCount * len(models.AllResolutions)
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated manifest: %w", err)
	}

	manifestKey := fmt.Sprintf("jobs/partition_%d/job_%s/job_manifest.json", pm.partitionID, jobID)
	err = pm.coord.objStore.PutObject(ctx, manifestKey, bytes.NewReader(manifestData), int64(len(manifestData)))
	if err != nil {
		return 0, fmt.Errorf("failed to upload updated manifest to S3: %w", err)
	}

	return segmentCount, nil
}

// streamSlice pipes the S3 stream directly into ffmpeg.
func (pm *PartitionManager) streamSlice(ctx context.Context, jobID string, prefix []byte, remaining io.Reader, tempDir string) (int, error) {
	sliceCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

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

	// Ensure process dies if coordinator crashes
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	pw, err := cmd.StdinPipe()
	if err != nil {
		return 0, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Run ffmpeg in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run()
	}()

	// Write prefix and stream to pipe
	go func() {
		defer pw.Close()
		pw.Write(prefix)
		io.Copy(pw, remaining)
	}()

	err = <-errCh
	if err != nil {
		return 0, fmt.Errorf("ffmpeg stream-slicing failed: %w", err)
	}

	return pm.uploadSlices(ctx, jobID, tempDir)
}

// downloadAndSlice downloads the raw file, corrects moov alignment, then slices.
func (pm *PartitionManager) downloadAndSlice(ctx context.Context, jobID string, prefix []byte, remaining io.Reader, tempDir string, sourcePath string) (int, error) {
	// Create local input file
	rawFile, err := os.CreateTemp("", "raw-input-*.mp4")
	if err != nil {
		return 0, fmt.Errorf("failed to create temp raw input file: %w", err)
	}
	defer os.Remove(rawFile.Name())
	defer rawFile.Close()

	// Write prefix and remaining
	rawFile.Write(prefix)
	_, err = io.Copy(rawFile, remaining)
	if err != nil {
		return 0, fmt.Errorf("failed to download raw file: %w", err)
	}
	rawFile.Close()

	// Run faststart relocation: ffmpeg -y -i input -c copy -movflags +faststart output
	faststartPath := filepath.Join(tempDir, "faststart.mp4")
	fsCtx, fsCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer fsCancel()

	fsCmd := exec.CommandContext(fsCtx, "ffmpeg", "-y", "-i", rawFile.Name(), "-c", "copy", "-movflags", "+faststart", faststartPath)
	if err := fsCmd.Run(); err != nil {
		return 0, fmt.Errorf("faststart relocation failed: %w", err)
	}

	// Slice the faststart output
	sliceCtx, sliceCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer sliceCancel()

	cmd := exec.CommandContext(sliceCtx, "ffmpeg",
		"-i", faststartPath,
		"-c", "copy",
		"-f", "segment",
		"-segment_format", "mp4",
		"-segment_time", "5",
		"-break_non_keyframes", "0",
		"-reset_timestamps", "1",
		filepath.Join(tempDir, "chunk_%03d.mp4"),
	)
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffmpeg slicing of faststart file failed: %w", err)
	}

	return pm.uploadSlices(ctx, jobID, tempDir)
}

func (pm *PartitionManager) uploadSlices(ctx context.Context, jobID string, tempDir string) (int, error) {
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read sliced directory: %w", err)
	}

	segmentCount := 0
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".mp4" || file.Name() == "faststart.mp4" {
			continue
		}

		filePath := filepath.Join(tempDir, file.Name())
		f, err := os.Open(filePath)
		if err != nil {
			return 0, fmt.Errorf("failed to open segment %s: %w", file.Name(), err)
		}

		stat, err := f.Stat()
		if err != nil {
			f.Close()
			return 0, fmt.Errorf("failed to stat segment %s: %w", file.Name(), err)
		}

		// Destination S3 key
		destKey := fmt.Sprintf("jobs/partition_%d/job_%s/raw/%s", pm.partitionID, jobID, file.Name())
		err = pm.coord.objStore.PutObject(ctx, destKey, f, stat.Size())
		f.Close()
		if err != nil {
			return 0, fmt.Errorf("failed to upload segment %s to S3: %w", file.Name(), err)
		}

		segmentCount++
	}

	if segmentCount == 0 {
		return 0, fmt.Errorf("no segments produced by ffmpeg")
	}

	return segmentCount, nil
}

func (pm *PartitionManager) markJobFailed(ctx context.Context, jobID, reason string) {
	pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
		"state":        string(models.JobPhaseFailed),
		"error":        reason,
		"last_updated": time.Now().Unix(),
	})
	pm.coord.state.PublishProgress(ctx, jobID, models.ProgressUpdate{Phase: models.JobPhaseFailed, Error: reason})

	// Clean up active jobs tracking in partition
	pm.coord.state.RemoveActiveJob(ctx, pm.partitionID, jobID)

	// Clean up raw files and slices from S3 to prevent disk leaks
	rawPrefix := fmt.Sprintf("jobs/partition_%d/job_%s/raw/", pm.partitionID, jobID)
	if err := pm.coord.objStore.DeletePrefix(ctx, rawPrefix); err != nil {
		log.Printf("Job %s: failed to clean up raw S3 files on failure: %v", jobID, err)
	}

	// Expire Redis keys after 24h to prevent memory leaks (fails open)
	if err := pm.coord.state.ExpireJobKeys(ctx, jobID, 86400); err != nil {
		log.Printf("Job %s: failed to set Redis keys expiration on failure: %v", jobID, err)
	}
}

func isFaststart(prefix []byte) bool {
	moovIdx := bytes.Index(prefix, []byte("moov"))
	mdatIdx := bytes.Index(prefix, []byte("mdat"))
	return moovIdx != -1 && (mdatIdx == -1 || moovIdx < mdatIdx)
}

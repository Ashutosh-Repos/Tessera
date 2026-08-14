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
	"time"

	"github.com/distributed-transcoder/internal/models"
	"golang.org/x/sync/errgroup"
)

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

	// 3. Execute stream-slicing, upload, and pipelined dispatch via ffmpeg
	// ISSUE-005 fix: uploadSlices now dispatches NATS tasks inline as each
	// chunk lands on S3, so workers begin transcoding immediately.
	segmentCount, err := pm.executeSlicing(ctx, jobID)
	if err != nil {
		log.Printf("Job %s: slicing failed: %v", jobID, err)
		pm.markJobFailed(ctx, jobID, err.Error())
		return
	}

	// 4. Update status to TRANSCODING and flush all pipelined NATS publishes
	manifest, mErr := pm.loadManifest(ctx, jobID)
	targetResolutions := models.AllResolutions
	if mErr == nil && len(manifest.Resolutions) > 0 {
		targetResolutions = manifest.Resolutions
	}
	totalTasks := segmentCount * len(targetResolutions)
	pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
		"state": string(models.JobPhaseTranscoding), "total": totalTasks,
		"last_updated": time.Now().Unix(),
	})
	pm.coord.bus.FlushPendingPublishes(ctx)
	pm.coord.state.PublishProgress(ctx, jobID, models.ProgressUpdate{Phase: models.JobPhaseTranscoding, Total: totalTasks})
}

func (pm *PartitionManager) executeSlicing(ctx context.Context, jobID string) (int, error) {
	// 1. Load job manifest to find SourcePath
	manifest, err := pm.loadManifest(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("failed to load manifest: %w", err)
	}
	if len(manifest.Resolutions) == 0 {
		manifest.Resolutions = models.AllResolutions
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

	// Read first 1MB for faststart moov atom check
	buf := make([]byte, 1024*1024)
	n, err := io.ReadFull(stream, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return 0, fmt.Errorf("failed to read stream prefix: %w", err)
	}
	prefix := buf[:n]

	isFast := IsFaststart(prefix)

	// Phase 1: FFmpeg slicing (produces chunk files in tempDir)
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

	// Phase 2: Duration calculation and asset generation BEFORE upload.
	// ISSUE-003 fix: uploadSlices now deletes chunk files from disk after
	// uploading each one. Duration probing and thumbnail extraction must
	// happen while files are still on disk.
	var totalDuration float64
	if segmentCount > 0 {
		lastChunkIdx := segmentCount - 1
		lastChunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", lastChunkIdx))
		lastChunkDur, err := getChunkDuration(ctx, lastChunkPath)
		if err == nil {
			totalDuration = float64(lastChunkIdx)*5.0 + lastChunkDur
		} else {
			totalDuration = float64(segmentCount) * 5.0
		}
	}

	// Generate thumbnails and preview sprites (reads chunk files from tempDir)
	if err := pm.generateAssets(ctx, jobID, tempDir, segmentCount, totalDuration); err != nil {
		log.Printf("Job %s: failed to generate assets: %v", jobID, err)
	}

	// Phase 3: Upload manifest to S3 with segment count and duration
	manifest.SegmentCount = segmentCount
	manifest.TotalTasks = segmentCount * len(manifest.Resolutions)
	manifest.DurationSec = totalDuration
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated manifest: %w", err)
	}

	manifestKey := fmt.Sprintf("jobs/partition_%d/job_%s/job_manifest.json", pm.partitionID, jobID)
	err = pm.coord.objStore.PutObject(ctx, manifestKey, bytes.NewReader(manifestData), int64(len(manifestData)))
	if err != nil {
		return 0, fmt.Errorf("failed to upload updated manifest to S3: %w", err)
	}

	// Phase 4: Parallel upload + pipelined NATS dispatch + disk cleanup.
	// ISSUE-005: Tasks are dispatched inline as each chunk uploads to S3.
	// ISSUE-003: Chunk files are removed from disk after each upload.
	uploadCount, err := pm.UploadSlices(ctx, jobID, tempDir)
	if err != nil {
		return 0, err
	}

	return uploadCount, nil
}

// streamSlice pipes the S3 stream directly into ffmpeg.
// Returns the segment count by counting output chunk files (does NOT upload).
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
	cmd.SysProcAttr = platformSysProcAttr()
	go platformParentWatchdog(sliceCtx, cmd)

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

	return CountChunkFiles(tempDir)
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
	fsCmd.SysProcAttr = platformSysProcAttr()
	go platformParentWatchdog(fsCtx, fsCmd)
	if err := fsCmd.Run(); err != nil {
		return 0, fmt.Errorf("faststart relocation failed: %w", err)
	}

	return pm.sliceFaststart(ctx, jobID, faststartPath, tempDir)
}

func (pm *PartitionManager) sliceFaststart(ctx context.Context, jobID string, faststartPath string, tempDir string) (int, error) {
	chunkPattern := filepath.Join(tempDir, "chunk_%03d.mp4")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-i", faststartPath,
		"-c", "copy",
		"-map", "0",
		"-f", "segment",
		"-segment_time", "5",
		"-reset_timestamps", "1",
		chunkPattern,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("ffmpeg faststart slicing failed: %s", string(output))
		return 0, fmt.Errorf("ffmpeg slicing of faststart file failed: %w", err)
	}

	return CountChunkFiles(tempDir)
}

func (pm *PartitionManager) UploadSlices(ctx context.Context, jobID string, tempDir string) (int, error) {
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read sliced directory: %w", err)
	}

	// Pre-filter to valid .mp4 chunk files only
	var chunkFiles []os.DirEntry
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".mp4" || file.Name() == "faststart.mp4" {
			continue
		}
		chunkFiles = append(chunkFiles, file)
	}

	if len(chunkFiles) == 0 {
		return 0, fmt.Errorf("no segments produced by ffmpeg")
	}

	// Pre-compute NATS shard for this partition (constant across all segments)
	denominator := pm.coord.cfg.Coordinator.PartitionCount / pm.coord.cfg.Coordinator.NATSShardCount
	var shard int
	if denominator <= 0 {
		shard = pm.partitionID % pm.coord.cfg.Coordinator.NATSShardCount
	} else {
		shard = pm.partitionID / denominator
	}

	// Upload chunks in parallel with bounded concurrency.
	// Limit of 10 saturates typical 1Gbps NIC without exhausting TCP sockets
	// or triggering S3/MinIO connection throttling.
	// If any single upload fails, gCtx is cancelled, aborting all in-flight uploads.
	//
	// ISSUE-005 fix: Each goroutine dispatches NATS transcode tasks inline
	// immediately after its PutObject succeeds. Workers begin transcoding
	// chunk_000 while chunk_010 is still uploading.
	//
	// ISSUE-003 fix: Each goroutine removes its chunk file from disk after
	// uploading, preventing SSD I/O accumulation. Asset generation and
	// duration probing have already completed before this function is called.
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(10)

	// Determine target resolutions from manifest if available
	targetResolutions := models.AllResolutions
	if manifest, err := pm.loadManifest(ctx, jobID); err == nil && len(manifest.Resolutions) > 0 {
		targetResolutions = manifest.Resolutions
	}

	for _, file := range chunkFiles {
		file := file // capture loop variable for goroutine closure
		g.Go(func() error {
			filePath := filepath.Join(tempDir, file.Name())
			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("failed to open segment %s: %w", file.Name(), err)
			}

			stat, err := f.Stat()
			if err != nil {
				f.Close()
				return fmt.Errorf("failed to stat segment %s: %w", file.Name(), err)
			}

			destKey := fmt.Sprintf("jobs/partition_%d/job_%s/raw/%s", pm.partitionID, jobID, file.Name())
			if err := pm.coord.objStore.PutObject(gCtx, destKey, f, stat.Size()); err != nil {
				f.Close()
				return fmt.Errorf("failed to upload segment %s to S3: %w", file.Name(), err)
			}
			f.Close()

			// ISSUE-005: Pipelined dispatch — publish transcode tasks to NATS
			// the instant this chunk lands on S3.
			var segIdx int
			if _, scanErr := fmt.Sscanf(file.Name(), "chunk_%03d.mp4", &segIdx); scanErr == nil {
				for _, res := range targetResolutions {
					task := models.SegmentTask{
						JobID:       jobID,
						PartitionID: pm.partitionID,
						OwnerEpoch:  pm.coord.CurrentEpoch,
						SegmentIdx:  segIdx,
						Resolution:  res,
						RawChunkKey: destKey,
						OutputKey:   fmt.Sprintf("jobs/partition_%d/job_%s/transcoded/segment_%03d_%s.ts", pm.partitionID, jobID, segIdx, res),
						HWAccel:     pm.coord.cfg.Worker.HWAccel,
						Priority:    "normal",
					}

					taskBytes, err := json.Marshal(task)
					if err != nil {
						return fmt.Errorf("failed to marshal task: %w", err)
					}

					if err := pm.coord.bus.PublishTaskAsync(ctx, shard, task.Priority, taskBytes); err != nil {
						return fmt.Errorf("failed to publish task: %w", err)
					}
				}
			}

			os.Remove(filePath)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return 0, fmt.Errorf("slice upload/dispatch failed: %w", err)
	}

	return len(chunkFiles), nil
}

// CountChunkFiles counts valid .mp4 chunk files in the given directory.
func CountChunkFiles(tempDir string) (int, error) {
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read sliced directory: %w", err)
	}
	count := 0
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".mp4" || file.Name() == "faststart.mp4" {
			continue
		}
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("no segments produced by ffmpeg")
	}
	return count, nil
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

func IsFaststart(prefix []byte) bool {
	moovIdx := bytes.Index(prefix, []byte("moov"))
	mdatIdx := bytes.Index(prefix, []byte("mdat"))
	return moovIdx != -1 && (mdatIdx == -1 || moovIdx < mdatIdx)
}

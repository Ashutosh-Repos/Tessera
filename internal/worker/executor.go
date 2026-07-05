package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
)

type TaskExecutor struct {
	state    infra.StateStore
	objStore infra.ObjectStore
	cfg      config.Config
	breaker  *CircuitBreaker
}

func NewTaskExecutor(state infra.StateStore, objStore infra.ObjectStore, cfg config.Config, breaker *CircuitBreaker) *TaskExecutor {
	return &TaskExecutor{
		state:    state,
		objStore: objStore,
		cfg:      cfg,
		breaker:  breaker,
	}
}

func (te *TaskExecutor) Execute(ctx context.Context, msg infra.TaskMessage, task models.SegmentTask) error {
	// ──── Step 1: Disk Quota Check ────
	if err := os.MkdirAll(te.cfg.Worker.ScratchDir, 0755); err != nil {
		msg.Nak()
		return fmt.Errorf("failed to create scratch directory: %w", err)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(te.cfg.Worker.ScratchDir, &stat); err != nil {
		msg.Nak()
		return fmt.Errorf("failed to stat scratch dir: %w", err)
	}
	freeGB := (stat.Bavail * uint64(stat.Bsize)) / (1024 * 1024 * 1024)
	if freeGB < uint64(te.cfg.Worker.MinDiskFreeGB) {
		msg.Nak() // re-queue to another worker
		return fmt.Errorf("disk quota exceeded: %d GB free", freeGB)
	}

	// ──── Step 2: Two-Tier Idempotency Check ────
	if te.checkIdempotency(ctx, task) {
		msg.Ack() // already completed
		return nil
	}

	// ──── Step 3: Download raw chunk ────
	localInput := filepath.Join(te.cfg.Worker.ScratchDir, fmt.Sprintf("%s_%d_%s.mp4", task.JobID, task.SegmentIdx, task.Resolution))
	if err := te.downloadFromS3(ctx, task.RawChunkKey, localInput); err != nil {
		msg.Nak()
		return err
	}
	defer os.Remove(localInput)

	// ──── Step 4: Transcode with FFmpeg ────
	localOutput := filepath.Join(te.cfg.Worker.ScratchDir, fmt.Sprintf("%s_%d_%s.ts", task.JobID, task.SegmentIdx, task.Resolution))
	defer os.Remove(localOutput)

	transcodeCtx, transcodeCancel := context.WithTimeout(ctx, time.Duration(te.cfg.Worker.MaxTaskDurationMin)*time.Minute)
	defer transcodeCancel()

	ffmpegArgs := te.buildFFmpegArgs(localInput, localOutput, task.Resolution)
	cmd := exec.CommandContext(transcodeCtx, "ffmpeg", ffmpegArgs...)
	cmd.SysProcAttr = platformSysProcAttr()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Launch macOS parent-death watchdog (no-op on Linux where Pdeathsig handles it)
	go platformParentWatchdog(transcodeCtx, cmd)

	// ──── Step 5: Launch Watchdog on dedicated OS thread ────
	watchdogDone := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(watchdogDone)
		te.runWatchdog(transcodeCtx, cmd, localOutput)
	}()

	// ──── Step 6: InProgress heartbeat (extend NATS AckWait) ────
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-transcodeCtx.Done():
				return
			case <-ticker.C:
				msg.InProgress() // reset AckWait deadline
			}
		}
	}()

	// ──── Step 7: Run FFmpeg ────
	if err := cmd.Start(); err != nil {
		transcodeCancel()
		<-watchdogDone
		<-heartbeatDone
		// Don't ACK — let NATS AckWait redeliver
		return fmt.Errorf("ffmpeg failed to start: %w", err)
	}

	cleanup := platformLimitProcess(cmd.Process.Pid)
	defer cleanup()

	err := cmd.Wait()
	transcodeCancel()
	<-watchdogDone
	<-heartbeatDone

	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w (stderr: %s)", err, stderr.String())
	}

	// ──── Step 8: Probe duration BEFORE upload (B-1 fix) ────
	duration := te.probeDuration(localOutput)

	// ──── Step 9: Upload to S3 (temporary path first) ────
	tempOutputKey := fmt.Sprintf("%s.%s.tmp", task.OutputKey, te.cfg.NodeID)
	f, err := os.Open(localOutput)
	if err != nil {
		return fmt.Errorf("failed to open local output %s: %w", localOutput, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat local output %s: %w", localOutput, err)
	}

	if err := te.objStore.PutObject(ctx, tempOutputKey, f, fi.Size()); err != nil {
		return fmt.Errorf("failed to upload transcode output to storage: %w", err)
	}

	// Atomic rename to canonical path (prevents double-commit)
	te.objStore.CopyObject(ctx, tempOutputKey, task.OutputKey)
	te.objStore.DeleteObject(ctx, tempOutputKey)

	// ──── Step 10: Redis Completion Pipeline (single RTT) ────
	te.state.ExecuteCompletionPipeline(ctx, infra.CompletionPipelineParams{
		JobID:      task.JobID,
		SegmentIdx: task.SegmentIdx,
		Resolution: string(task.Resolution),
		BitIndex:   task.BitIndex(),
		Duration:   duration,
		UnixNow:    time.Now().Unix(),
		Completed:  1, // the worker doesn't know total completed, the pipeline handles INCR
	})

	// ──── Step 11: ACK ────
	msg.Ack()
	return nil
}

func (te *TaskExecutor) checkIdempotency(ctx context.Context, task models.SegmentTask) bool {
	// Fast path: Redis EXISTS (< 0.1ms)
	if !te.breaker.IsOpen() {
		exists, err := te.state.TaskExists(ctx, task.JobID, task.SegmentIdx, string(task.Resolution))
		if err != nil {
			te.breaker.RecordFailure()
			// Fall through to S3
		} else {
			te.breaker.RecordSuccess()
			if exists {
				return true // already done
			}
			return false // not done, proceed with transcoding
		}
	}

	// Circuit breaker is open — apply backoff before S3 fallback
	if te.breaker.IsOpen() {
		time.Sleep(te.breaker.BackoffDuration())
	}

	// Slow path: S3 HeadObject (5-10ms)
	meta, err := te.objStore.HeadObject(ctx, task.OutputKey)
	if err != nil {
		return false // assume not done
	}
	return meta.Exists
}

func (te *TaskExecutor) downloadFromS3(ctx context.Context, key, localPath string) error {
	rc, err := te.objStore.GetObject(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, rc)
	return err
}

func (te *TaskExecutor) probeDuration(filePath string) string {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	).Output()
	if err != nil {
		return "0"
	}
	return strings.TrimSpace(string(out))
}

func (te *TaskExecutor) runWatchdog(ctx context.Context, cmd *exec.Cmd, outputPath string) {
	ticker := time.NewTicker(time.Duration(te.cfg.Worker.WatchdogIntervalSec) * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	var lastSize int64
	var stalledTicks int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 1. Check max execution duration (I-14 fix)
			if time.Since(startTime) > time.Duration(te.cfg.Worker.MaxTaskDurationMin)*time.Minute {
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return
			}

			fi, err := os.Stat(outputPath)
			if err != nil {
				continue // file not yet created
			}
			currentSize := fi.Size()

			// 2. Check max temp file size (I-14 fix)
			maxSizeBytes := int64(te.cfg.Worker.MaxTempFileSizeGB) * 1024 * 1024 * 1024
			if currentSize > maxSizeBytes {
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return
			}

			// 3. Check for stalled process
			if currentSize == lastSize && lastSize > 0 {
				stalledTicks++
				if stalledTicks >= 5 { // 5 ticks * 2s = 10s of no progress
					// No progress → FFmpeg stalled → kill it
					if cmd.Process != nil {
						syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // kill process group
					}
					return
				}
			} else {
				stalledTicks = 0
			}
			lastSize = currentSize
		}
	}
}

func (te *TaskExecutor) buildFFmpegArgs(input, output string, res models.Resolution) []string {
	// Resolution presets
	presets := map[models.Resolution][]string{
		models.Res1080p: {"-vf", "scale=1920:1080", "-b:v", "5000k"},
		models.Res720p:  {"-vf", "scale=1280:720", "-b:v", "2800k"},
		models.Res480p:  {"-vf", "scale=854:480", "-b:v", "1400k"},
	}

	var args []string

	// Hardware acceleration input options must precede -i <input>
	switch te.cfg.Worker.HWAccel {
	case "nvenc":
		args = append(args, "-hwaccel", "cuda")
	case "vaapi":
		args = append(args, "-hwaccel", "vaapi")
	}

	args = append(args, "-i", input)

	// Video encoder selection
	switch te.cfg.Worker.HWAccel {
	case "nvenc":
		args = append(args, "-c:v", "h264_nvenc")
	case "vaapi":
		args = append(args, "-c:v", "h264_vaapi")
	case "videotoolbox":
		args = append(args, "-c:v", "h264_videotoolbox")
	default:
		args = append(args, "-c:v", "libx264", "-preset", "fast")
	}

	args = append(args, presets[res]...)
	args = append(args,
		"-c:a", "aac", "-b:a", "128k",
		"-copyts",                            // preserve presentation timestamps
		"-force_key_frames", "expr:gte(t,0)", // align keyframes across resolutions
		"-f", "mpegts",
		"-y", output,
	)
	return args
}

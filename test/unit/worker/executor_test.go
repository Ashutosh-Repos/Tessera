package worker_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/internal/worker"
	"github.com/distributed-transcoder/test/mocks"
)

func TestIdempotencyCheck_ZeroByteS3Object(t *testing.T) {
	stateStore := mocks.NewMockStateStore()
	objStore := mocks.NewMockObjectStore()
	breaker := worker.NewCircuitBreaker(5, 3)
	cfg := config.Config{}

	executor := worker.NewTaskExecutor(stateStore, objStore, cfg, breaker)

	task := models.SegmentTask{
		JobID:      "us-east:test-job-123",
		SegmentIdx: 0,
		Resolution: models.Res1080p,
		OutputKey:  "jobs/partition_1/job_123/transcoded/segment_000_1080p.ts",
	}

	stateStore.Err = context.DeadlineExceeded
	objStore.Objects[task.OutputKey] = []byte{}

	ctx := context.Background()

	stateStore.TaskDoneMap["us-east:test-job-123:0:1080p"] = false

	exists := executor.CheckIdempotency(ctx, task)
	if exists {
		t.Errorf("checkIdempotency() = true for 0-byte S3 object, want false (corrupted file must be re-transcoded)")
	}
}

func TestIdempotencyCheck_ValidS3Object(t *testing.T) {
	stateStore := mocks.NewMockStateStore()
	objStore := mocks.NewMockObjectStore()
	breaker := worker.NewCircuitBreaker(5, 3)
	cfg := config.Config{}

	executor := worker.NewTaskExecutor(stateStore, objStore, cfg, breaker)

	task := models.SegmentTask{
		JobID:      "us-east:test-job-123",
		SegmentIdx: 0,
		Resolution: models.Res1080p,
		OutputKey:  "jobs/partition_1/job_123/transcoded/segment_000_1080p.ts",
	}

	stateStore.Err = context.DeadlineExceeded
	objStore.Objects[task.OutputKey] = []byte("valid-ts-segment-content")

	ctx := context.Background()

	exists := executor.CheckIdempotency(ctx, task)
	if !exists {
		t.Errorf("checkIdempotency() = false for valid >0 byte S3 object, want true")
	}
}

func TestDirectUpload_SinglePutNoCopyDelete(t *testing.T) {
	stateStore := mocks.NewMockStateStore()
	objStore := mocks.NewMockObjectStore()
	breaker := worker.NewCircuitBreaker(5, 3)
	cfg := config.Config{}

	executor := worker.NewTaskExecutor(stateStore, objStore, cfg, breaker)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "segment_000_1080p.ts")
	content := []byte("transcoded-ts-video-data-bytes")
	if err := os.WriteFile(outPath, content, 0644); err != nil {
		t.Fatalf("failed to write test output file: %v", err)
	}

	task := models.SegmentTask{
		JobID:      "us-east:job-direct-upload",
		SegmentIdx: 0,
		Resolution: models.Res1080p,
		OutputKey:  "jobs/partition_5/job_direct/transcoded/segment_000_1080p.ts",
	}

	ctx := context.Background()

	err := executor.CommitTranscodedOutput(ctx, outPath, task)
	if err != nil {
		t.Fatalf("commitTranscodedOutput() failed: %v", err)
	}

	if objStore.PutCount != 1 {
		t.Errorf("objStore.PutCount = %d, want 1 (direct PutObject)", objStore.PutCount)
	}
	if objStore.CopyCount != 0 {
		t.Errorf("objStore.CopyCount = %d, want 0 (CopyObject must be eliminated)", objStore.CopyCount)
	}
	if objStore.DeleteCount != 0 {
		t.Errorf("objStore.DeleteCount = %d, want 0 (DeleteObject must be eliminated)", objStore.DeleteCount)
	}

	uploadedData, exists := objStore.Objects[task.OutputKey]
	if !exists {
		t.Fatalf("OutputKey %q was not uploaded to S3", task.OutputKey)
	}
	if !bytes.Equal(uploadedData, content) {
		t.Errorf("uploaded content mismatch: got %q, want %q", string(uploadedData), string(content))
	}
}

func TestUploadSizeGuard_RejectsZeroByteFile(t *testing.T) {
	stateStore := mocks.NewMockStateStore()
	objStore := mocks.NewMockObjectStore()
	breaker := worker.NewCircuitBreaker(5, 3)
	cfg := config.Config{}

	executor := worker.NewTaskExecutor(stateStore, objStore, cfg, breaker)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "corrupted_zero_byte.ts")
	if err := os.WriteFile(outPath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to write 0-byte file: %v", err)
	}

	task := models.SegmentTask{
		JobID:      "us-east:job-zero-byte",
		SegmentIdx: 1,
		Resolution: models.Res720p,
		OutputKey:  "jobs/partition_5/job_zero/transcoded/segment_001_720p.ts",
	}

	ctx := context.Background()

	err := executor.CommitTranscodedOutput(ctx, outPath, task)
	if err == nil {
		t.Fatalf("commitTranscodedOutput() succeeded for 0-byte file, want error (size guard)")
	}

	if objStore.PutCount != 0 {
		t.Errorf("objStore.PutCount = %d, want 0 (0-byte corrupted output must not be uploaded to S3)", objStore.PutCount)
	}
}

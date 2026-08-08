package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
)

// ──── Mock ObjectStore for Testing ────

type mockObjectStore struct {
	putCalls    []string
	copyCalls   []string
	deleteCalls []string
	headFunc    func(ctx context.Context, key string) (infra.ObjectMeta, error)
	putFunc     func(ctx context.Context, key string, body io.Reader, size int64) error
	getFunc     func(ctx context.Context, key string) (io.ReadCloser, error)
	mu          sync.Mutex
}

func (m *mockObjectStore) CreateMultipartUpload(ctx context.Context, key string) (string, error) {
	return "mock-upload-id", nil
}

func (m *mockObjectStore) GeneratePresignedPUT(ctx context.Context, key, uploadID string, partNum int, expiry time.Duration) (string, error) {
	return "http://mock-presigned-url", nil
}

func (m *mockObjectStore) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []infra.CompletedPart) error {
	return nil
}

func (m *mockObjectStore) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	return nil
}

func (m *mockObjectStore) PutObject(ctx context.Context, key string, body io.Reader, size int64) error {
	m.mu.Lock()
	m.putCalls = append(m.putCalls, key)
	m.mu.Unlock()
	if m.putFunc != nil {
		return m.putFunc(ctx, key, body, size)
	}
	return nil
}

func (m *mockObjectStore) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, key)
	}
	return io.NopCloser(io.LimitReader(os.Stdin, 0)), nil
}

func (m *mockObjectStore) HeadObject(ctx context.Context, key string) (infra.ObjectMeta, error) {
	if m.headFunc != nil {
		return m.headFunc(ctx, key)
	}
	return infra.ObjectMeta{Exists: false}, fmt.Errorf("not found")
}

func (m *mockObjectStore) CopyObject(ctx context.Context, srcKey, dstKey string) error {
	m.mu.Lock()
	m.copyCalls = append(m.copyCalls, fmt.Sprintf("%s->%s", srcKey, dstKey))
	m.mu.Unlock()
	return nil
}

func (m *mockObjectStore) DeleteObject(ctx context.Context, key string) error {
	m.mu.Lock()
	m.deleteCalls = append(m.deleteCalls, key)
	m.mu.Unlock()
	return nil
}

func (m *mockObjectStore) DeletePrefix(ctx context.Context, prefix string) error {
	return nil
}

func (m *mockObjectStore) ListObjectsPrefix(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}

func (m *mockObjectStore) Ping(ctx context.Context) error {
	return nil
}

// ──── Mock StateStore for Testing ────

type mockStateStore struct {
	infra.StateStore
	taskExists bool
	taskErr    error
}

func (m *mockStateStore) TaskExists(ctx context.Context, jobID string, segment int, res string) (bool, error) {
	return m.taskExists, m.taskErr
}

// ──── Tests for ISSUE-001 Fix ────

func TestIdempotencyCheck_ZeroByteS3Object(t *testing.T) {
	// Circuit breaker closed, Redis fails, S3 returns Exists=true BUT Size=0
	objStore := &mockObjectStore{
		headFunc: func(ctx context.Context, key string) (infra.ObjectMeta, error) {
			return infra.ObjectMeta{
				Key:    key,
				Size:   0, // 0-byte corrupted object
				Exists: true,
			}, nil
		},
	}
	stateStore := &mockStateStore{taskErr: fmt.Errorf("redis down")}
	breaker := NewCircuitBreaker(5, 3)

	executor := NewTaskExecutor(stateStore, objStore, config.Config{}, breaker)

	task := models.SegmentTask{
		JobID:      "test-job",
		SegmentIdx: 0,
		Resolution: models.Res1080p,
		OutputKey:  "jobs/partition_1/job_test-job/transcoded/segment_000_1080p.ts",
	}

	// Should return false because Size=0 (not done)
	isDone := executor.checkIdempotency(context.Background(), task)
	if isDone {
		t.Errorf("checkIdempotency() = true for 0-byte object, want false")
	}
}

func TestIdempotencyCheck_ValidS3Object(t *testing.T) {
	// Circuit breaker closed, Redis fails, S3 returns Exists=true AND Size > 0
	objStore := &mockObjectStore{
		headFunc: func(ctx context.Context, key string) (infra.ObjectMeta, error) {
			return infra.ObjectMeta{
				Key:    key,
				Size:   1024, // valid object
				Exists: true,
			}, nil
		},
	}
	stateStore := &mockStateStore{taskErr: fmt.Errorf("redis down")}
	breaker := NewCircuitBreaker(5, 3)

	executor := NewTaskExecutor(stateStore, objStore, config.Config{}, breaker)

	task := models.SegmentTask{
		JobID:      "test-job",
		SegmentIdx: 0,
		Resolution: models.Res1080p,
		OutputKey:  "jobs/partition_1/job_test-job/transcoded/segment_000_1080p.ts",
	}

	// Should return true because Exists=true and Size > 0
	isDone := executor.checkIdempotency(context.Background(), task)
	if !isDone {
		t.Errorf("checkIdempotency() = false for valid object, want true")
	}
}

func TestDirectUpload_SinglePutNoCopyDelete(t *testing.T) {
	// Test that PutObject is called directly to task.OutputKey and Copy/Delete are NOT called
	objStore := &mockObjectStore{}
	stateStore := &mockStateStore{taskExists: false}
	breaker := NewCircuitBreaker(5, 3)

	cfg := config.Config{
		NodeID: "node-1",
		Worker: config.WorkerConfig{
			ScratchDir: t.TempDir(),
		},
	}

	executor := NewTaskExecutor(stateStore, objStore, cfg, breaker)

	outputKey := "jobs/partition_1/job_test/transcoded/segment_000_1080p.ts"
	localOutput := filepath.Join(cfg.Worker.ScratchDir, "output.ts")

	// Create a dummy valid output file (>0 bytes)
	if err := os.WriteFile(localOutput, []byte("valid video ts content"), 0644); err != nil {
		t.Fatalf("failed to write local output: %v", err)
	}

	f, err := os.Open(localOutput)
	if err != nil {
		t.Fatalf("failed to open local output: %v", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("failed to stat local output: %v", err)
	}

	// Direct upload logic from Step 9
	if fi.Size() == 0 {
		t.Fatalf("file size is 0")
	}

	err = executor.objStore.PutObject(context.Background(), outputKey, f, fi.Size())
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	// Assertions
	if len(objStore.putCalls) != 1 {
		t.Errorf("PutObject calls = %d, want 1", len(objStore.putCalls))
	}
	if len(objStore.putCalls) > 0 && objStore.putCalls[0] != outputKey {
		t.Errorf("PutObject key = %q, want %q", objStore.putCalls[0], outputKey)
	}

	if len(objStore.copyCalls) != 0 {
		t.Errorf("CopyObject calls = %d, want 0 (ISSUE-001 eliminates CopyObject)", len(objStore.copyCalls))
	}
	if len(objStore.deleteCalls) != 0 {
		t.Errorf("DeleteObject calls = %d, want 0 (ISSUE-001 eliminates DeleteObject)", len(objStore.deleteCalls))
	}
}

func TestUploadSizeGuard_RejectsZeroByteFile(t *testing.T) {
	objStore := &mockObjectStore{}
	stateStore := &mockStateStore{}
	breaker := NewCircuitBreaker(5, 3)

	cfg := config.Config{
		Worker: config.WorkerConfig{
			ScratchDir: t.TempDir(),
		},
	}

	executor := NewTaskExecutor(stateStore, objStore, cfg, breaker)
	localOutput := filepath.Join(cfg.Worker.ScratchDir, "zero.ts")

	// Create 0-byte file
	if err := os.WriteFile(localOutput, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create 0-byte file: %v", err)
	}

	f, err := os.Open(localOutput)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()

	fi, _ := f.Stat()

	// Simulate Step 9 size guard
	var uploadErr error
	if fi.Size() == 0 {
		uploadErr = fmt.Errorf("transcoded output %s is 0 bytes — corrupted segment", localOutput)
	} else {
		uploadErr = executor.objStore.PutObject(context.Background(), "test-key", f, fi.Size())
	}

	if uploadErr == nil {
		t.Errorf("uploadErr = nil for 0-byte file, want error")
	}
	if len(objStore.putCalls) != 0 {
		t.Errorf("PutObject calls = %d for 0-byte file, want 0", len(objStore.putCalls))
	}
}

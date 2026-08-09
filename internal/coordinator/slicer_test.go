package coordinator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/test/mocks"
)

func TestUploadSlices_ParallelSuccess(t *testing.T) {
	// 1. Setup temp directory with 25 chunk files
	tempDir, err := os.MkdirTemp("", "test-upload-slices-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	numChunks := 25
	for i := 0; i < numChunks; i++ {
		chunkName := fmt.Sprintf("chunk_%03d.mp4", i)
		chunkPath := filepath.Join(tempDir, chunkName)
		content := []byte(fmt.Sprintf("dummy video data for segment %d", i))
		if err := os.WriteFile(chunkPath, content, 0644); err != nil {
			t.Fatalf("failed to write dummy chunk file %s: %v", chunkName, err)
		}
	}

	// Add files that should be ignored by uploadSlices
	_ = os.WriteFile(filepath.Join(tempDir, "faststart.mp4"), []byte("faststart data"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "notes.txt"), []byte("text file"), 0644)
	_ = os.MkdirAll(filepath.Join(tempDir, "subdir"), 0755)

	// 2. Setup mock object store and PartitionManager
	mockStore := mocks.NewMockObjectStore()
	cfg := config.Config{}
	daemon := NewCoordinatorDaemon(cfg, "node-1", nil, nil, nil, mockStore)
	pm := NewPartitionManager(daemon, 42, 1)

	// 3. Execute uploadSlices
	ctx := context.Background()
	jobID := "test-job-parallel-123"
	count, err := pm.uploadSlices(ctx, jobID, tempDir)
	if err != nil {
		t.Fatalf("expected uploadSlices to succeed, got error: %v", err)
	}

	// 4. Verify segment count returned
	if count != numChunks {
		t.Errorf("expected segment count %d, got %d", numChunks, count)
	}

	// 5. Verify all chunks were uploaded to the mock store
	if mockStore.PutCount != numChunks {
		t.Errorf("expected PutCount %d in mock store, got %d", numChunks, mockStore.PutCount)
	}

	for i := 0; i < numChunks; i++ {
		expectedKey := fmt.Sprintf("jobs/partition_42/job_%s/raw/chunk_%03d.mp4", jobID, i)
		data, exists := mockStore.Objects[expectedKey]
		if !exists {
			t.Errorf("expected key %s to exist in mock store, but missing", expectedKey)
		} else {
			expectedContent := fmt.Sprintf("dummy video data for segment %d", i)
			if string(data) != expectedContent {
				t.Errorf("content mismatch for %s: expected %q, got %q", expectedKey, expectedContent, string(data))
			}
		}
	}
}

func TestUploadSlices_NoSegmentsError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-upload-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Only non-chunk files present
	_ = os.WriteFile(filepath.Join(tempDir, "faststart.mp4"), []byte("faststart"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "info.json"), []byte("{}"), 0644)

	mockStore := mocks.NewMockObjectStore()
	daemon := NewCoordinatorDaemon(config.Config{}, "node-1", nil, nil, nil, mockStore)
	pm := NewPartitionManager(daemon, 1, 1)

	_, err = pm.uploadSlices(context.Background(), "job-empty", tempDir)
	if err == nil {
		t.Fatal("expected error when no .mp4 chunks exist, got nil")
	}
}

func TestUploadSlices_S3ErrorPropagation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-upload-err-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	for i := 0; i < 5; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", i))
		_ = os.WriteFile(chunkPath, []byte("data"), 0644)
	}

	mockStore := mocks.NewMockObjectStore()
	mockStore.Err = errors.New("s3 connection failed")
	daemon := NewCoordinatorDaemon(config.Config{}, "node-1", nil, nil, nil, mockStore)
	pm := NewPartitionManager(daemon, 1, 1)

	_, err = pm.uploadSlices(context.Background(), "job-err", tempDir)
	if err == nil {
		t.Fatal("expected error when S3 upload fails, got nil")
	}
}

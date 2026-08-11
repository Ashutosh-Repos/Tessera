package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/test/mocks"
)

// newTestPM creates a PartitionManager wired with mock object store and message
// bus for unit testing uploadSlices. Caller provides overrides via opts.
func newTestPM(mockStore *mocks.MockObjectStore, mockBus *mocks.MockMessageBus) *PartitionManager {
	cfg := config.Config{}
	cfg.Coordinator.PartitionCount = 4
	cfg.Coordinator.NATSShardCount = 2
	daemon := NewCoordinatorDaemon(cfg, "node-1", nil, mockBus, nil, mockStore)
	daemon.currentEpoch = 1
	return NewPartitionManager(daemon, 2, 1)
}

// --- ISSUE-006 regression: parallel upload still works ---

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

	// 2. Setup mocks
	mockStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()
	pm := newTestPM(mockStore, mockBus)

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
		expectedKey := fmt.Sprintf("jobs/partition_2/job_%s/raw/chunk_%03d.mp4", jobID, i)
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
	mockBus := mocks.NewMockMessageBus()
	pm := newTestPM(mockStore, mockBus)

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
	mockBus := mocks.NewMockMessageBus()
	pm := newTestPM(mockStore, mockBus)

	_, err = pm.uploadSlices(context.Background(), "job-err", tempDir)
	if err == nil {
		t.Fatal("expected error when S3 upload fails, got nil")
	}
}

// --- ISSUE-005: Pipelined dispatch ---

func TestUploadSlices_PipelinedDispatch(t *testing.T) {
	// Verify that uploadSlices dispatches NATS tasks inline for each chunk.
	tempDir, err := os.MkdirTemp("", "test-pipelined-dispatch-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	numChunks := 10
	for i := 0; i < numChunks; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", i))
		_ = os.WriteFile(chunkPath, []byte(fmt.Sprintf("segment-%d", i)), 0644)
	}

	mockStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()
	pm := newTestPM(mockStore, mockBus)

	jobID := "dispatch-test-job"
	count, err := pm.uploadSlices(context.Background(), jobID, tempDir)
	if err != nil {
		t.Fatalf("uploadSlices failed: %v", err)
	}
	if count != numChunks {
		t.Fatalf("expected %d segments, got %d", numChunks, count)
	}

	// Verify: each chunk should have dispatched len(AllResolutions) tasks
	expectedTasks := numChunks * len(models.AllResolutions)
	totalTasks := 0
	for _, msgs := range mockBus.Tasks {
		totalTasks += len(msgs)
	}
	if totalTasks != expectedTasks {
		t.Errorf("expected %d NATS tasks dispatched, got %d", expectedTasks, totalTasks)
	}

	// Verify that dispatched tasks have correct fields
	segmentsSeen := make(map[int]bool)
	for _, msgs := range mockBus.Tasks {
		for _, msg := range msgs {
			var task models.SegmentTask
			if err := json.Unmarshal(msg.Data(), &task); err != nil {
				t.Fatalf("failed to unmarshal dispatched task: %v", err)
			}
			if task.JobID != jobID {
				t.Errorf("expected JobID %q, got %q", jobID, task.JobID)
			}
			if task.PartitionID != 2 {
				t.Errorf("expected PartitionID 2, got %d", task.PartitionID)
			}
			if task.OwnerEpoch != 1 {
				t.Errorf("expected OwnerEpoch 1, got %d", task.OwnerEpoch)
			}
			segmentsSeen[task.SegmentIdx] = true
		}
	}

	// Every segment index [0, numChunks) should have been dispatched
	for i := 0; i < numChunks; i++ {
		if !segmentsSeen[i] {
			t.Errorf("segment index %d was not dispatched", i)
		}
	}
}

// --- ISSUE-003: Disk staging cleanup ---

func TestUploadSlices_DiskCleanup(t *testing.T) {
	// Verify that uploadSlices removes chunk files from disk after uploading.
	tempDir, err := os.MkdirTemp("", "test-disk-cleanup-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	numChunks := 8
	for i := 0; i < numChunks; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", i))
		_ = os.WriteFile(chunkPath, []byte(fmt.Sprintf("video-data-%d", i)), 0644)
	}

	// Also add a non-chunk file that should NOT be deleted
	nonChunkPath := filepath.Join(tempDir, "sprite.jpg")
	_ = os.WriteFile(nonChunkPath, []byte("sprite-data"), 0644)

	mockStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()
	pm := newTestPM(mockStore, mockBus)

	_, err = pm.uploadSlices(context.Background(), "cleanup-test-job", tempDir)
	if err != nil {
		t.Fatalf("uploadSlices failed: %v", err)
	}

	// Verify: all chunk files should be deleted from disk
	for i := 0; i < numChunks; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", i))
		if _, err := os.Stat(chunkPath); !os.IsNotExist(err) {
			t.Errorf("chunk file %s should have been deleted from disk after upload, but still exists", chunkPath)
		}
	}

	// Verify: non-chunk file should still exist
	if _, err := os.Stat(nonChunkPath); os.IsNotExist(err) {
		t.Error("non-chunk file sprite.jpg should NOT have been deleted, but was")
	}

	// Verify: all data was actually uploaded to S3 before deletion
	if mockStore.PutCount != numChunks {
		t.Errorf("expected %d uploads, got %d", numChunks, mockStore.PutCount)
	}
}

// --- countChunkFiles ---

func TestCountChunkFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-count-chunks-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 12 valid chunks + noise files
	for i := 0; i < 12; i++ {
		_ = os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", i)), []byte("d"), 0644)
	}
	_ = os.WriteFile(filepath.Join(tempDir, "faststart.mp4"), []byte("f"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "notes.txt"), []byte("n"), 0644)
	_ = os.MkdirAll(filepath.Join(tempDir, "subdir"), 0755)

	count, err := countChunkFiles(tempDir)
	if err != nil {
		t.Fatalf("countChunkFiles failed: %v", err)
	}
	if count != 12 {
		t.Errorf("expected 12 chunk files, got %d", count)
	}
}

func TestCountChunkFiles_Empty(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-count-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	_ = os.WriteFile(filepath.Join(tempDir, "faststart.mp4"), []byte("f"), 0644)

	_, err = countChunkFiles(tempDir)
	if err == nil {
		t.Fatal("expected error for empty chunk directory, got nil")
	}
}

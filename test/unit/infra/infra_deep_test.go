package infra_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/test/mocks"
)

// ─────────────────────────────────────────────────────────────
// 1. Redis Cluster Hash-Tagging & Key Pattern Tests
// ─────────────────────────────────────────────────────────────

func Test_Infra_Redis_ClusterHashTagging_AllKeysIncludeJobBraces(t *testing.T) {
	jobID := "us-east-1:job-uuid-789"

	statusKey := fmt.Sprintf("job:{%s}:status", jobID)
	bitmapKey := fmt.Sprintf("job:{%s}:progress", jobID)
	durationsKey := fmt.Sprintf("job:{%s}:durations", jobID)
	manifestKey := fmt.Sprintf("job:{%s}:manifest", jobID)
	streamKey := fmt.Sprintf("job:{%s}:progress_stream", jobID)

	keys := []string{statusKey, bitmapKey, durationsKey, manifestKey, streamKey}

	for _, k := range keys {
		if !strings.Contains(k, fmt.Sprintf("{%s}", jobID)) {
			t.Errorf("key %q violates Redis Cluster hash-tagging pattern", k)
		}
	}
}

func Test_Infra_Redis_Bitmap_BitIndexCalculation_Valid(t *testing.T) {
	tests := []struct {
		segmentIdx int
		res        models.Resolution
		wantIndex  int
	}{
		{0, models.Res1080p, 0},
		{0, models.Res720p, 1},
		{0, models.Res480p, 2},
		{1, models.Res1080p, 3},
		{1, models.Res720p, 4},
		{1, models.Res480p, 5},
		{100, models.Res1080p, 300},
		{100, models.Res720p, 301},
		{100, models.Res480p, 302},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Segment_%d_%s", tt.segmentIdx, tt.res), func(t *testing.T) {
			task := models.SegmentTask{
				SegmentIdx: tt.segmentIdx,
				Resolution: tt.res,
			}
			if got := task.BitIndex(); got != tt.wantIndex {
				t.Errorf("BitIndex() = %d, want %d", got, tt.wantIndex)
			}
		})
	}
}

func Test_Infra_Redis_StreamKey_TrimPrefixFormats(t *testing.T) {
	rawKeys := []string{
		"job:{us-east-1:job-123}:progress_stream",
		"job:{job-456}:progress_stream",
		"{job-789}",
	}

	for _, raw := range rawKeys {
		cleaned := strings.TrimPrefix(raw, "job:{")
		cleaned = strings.TrimSuffix(cleaned, "}:progress_stream")
		cleaned = strings.TrimPrefix(cleaned, "{")
		cleaned = strings.TrimSuffix(cleaned, "}")

		if strings.Contains(cleaned, "{") || strings.Contains(cleaned, "}") || strings.Contains(cleaned, "job:") {
			t.Errorf("key was not properly cleaned: got %q from %q", cleaned, raw)
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 2. SQS Partition-Scoped Routing & DLQ
// ─────────────────────────────────────────────────────────────

func Test_Infra_SQS_PartitionQueueRouting(t *testing.T) {
	partitionID := 7
	uploadQueue := fmt.Sprintf("transcoder-upload-events-partition_%d", partitionID)
	completionQueue := fmt.Sprintf("transcoder-completion-events-partition_%d", partitionID)
	dlqQueue := "transcoder-dlq"

	if uploadQueue != "transcoder-upload-events-partition_7" {
		t.Errorf("unexpected upload queue: %s", uploadQueue)
	}
	if completionQueue != "transcoder-completion-events-partition_7" {
		t.Errorf("unexpected completion queue: %s", completionQueue)
	}
	if dlqQueue != "transcoder-dlq" {
		t.Errorf("unexpected dlq queue: %s", dlqQueue)
	}
}

// ─────────────────────────────────────────────────────────────
// 3. S3 Multi-Object Batch Delete & Prefix Cleaning
// ─────────────────────────────────────────────────────────────

func Test_Infra_S3_DeletePrefix_BatchingOver1000Objects(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	ctx := context.Background()

	prefix := "jobs/partition_3/job_scale_1000/raw/"
	totalObjects := 1500

	for i := 0; i < totalObjects; i++ {
		key := fmt.Sprintf("%schunk_%04d.mp4", prefix, i)
		_ = mockStore.PutObject(ctx, key, nil, 10)
	}

	if len(mockStore.Objects) != totalObjects {
		t.Fatalf("expected %d objects in mock store, got %d", totalObjects, len(mockStore.Objects))
	}

	err := mockStore.DeletePrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("DeletePrefix failed: %v", err)
	}

	if len(mockStore.Objects) != 0 {
		t.Errorf("expected all 1500 objects under prefix to be deleted, remaining: %d", len(mockStore.Objects))
	}
}

func Test_Infra_S3_PresignedPUT_IncludesPartQuery(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	ctx := context.Background()

	url, err := mockStore.GeneratePresignedPUT(ctx, "jobs/raw/test.mp4", "upload-123", 42, 15*time.Minute)
	if err != nil {
		t.Fatalf("GeneratePresignedPUT failed: %v", err)
	}

	if !strings.Contains(url, "part=42") {
		t.Errorf("expected presigned URL to contain part=42, got %s", url)
	}
}

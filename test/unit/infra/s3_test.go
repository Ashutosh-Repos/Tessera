package infra_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/test/mocks"
)

func TestObjectMeta_Properties(t *testing.T) {
	now := time.Now()
	meta := infra.ObjectMeta{
		Key:          "jobs/partition_0/job_1/raw/chunk_000.mp4",
		Size:         1024 * 1024 * 5, // 5MB
		LastModified: now,
		Exists:       true,
	}

	if !meta.Exists {
		t.Errorf("expected meta.Exists = true")
	}
	if meta.Size != 5242880 {
		t.Errorf("expected size 5242880, got %d", meta.Size)
	}
	if meta.Key != "jobs/partition_0/job_1/raw/chunk_000.mp4" {
		t.Errorf("key mismatch: %s", meta.Key)
	}
}

func TestMockObjectStore_DeletePrefix(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	ctx := context.Background()

	// Seed objects
	prefix := "jobs/partition_5/job_abc/raw/"
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("%schunk_%03d.mp4", prefix, i)
		_ = mockStore.PutObject(ctx, key, nil, 100)
	}
	// Seed other object outside prefix
	otherKey := "jobs/partition_5/job_abc/transcoded/segment_000_1080p.ts"
	_ = mockStore.PutObject(ctx, otherKey, nil, 200)

	if len(mockStore.Objects) != 11 {
		t.Fatalf("expected 11 objects in store, got %d", len(mockStore.Objects))
	}

	// Delete prefix
	err := mockStore.DeletePrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("DeletePrefix failed: %v", err)
	}

	if len(mockStore.Objects) != 1 {
		t.Errorf("expected 1 object remaining after prefix deletion, got %d", len(mockStore.Objects))
	}
	if _, exists := mockStore.Objects[otherKey]; !exists {
		t.Errorf("expected %s to remain intact", otherKey)
	}
}

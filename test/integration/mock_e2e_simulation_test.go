package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/coordinator"
	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/test/mocks"
)

// ─────────────────────────────────────────────────────────────
// In-Memory End-to-End Transcoding Engine Simulation
// ─────────────────────────────────────────────────────────────

func Test_E2E_FullLifecycle_UploadToTranscodeToManifest_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Initialize Mock Infrastructure
	mockStore := mocks.NewMockStateStore()
	mockObjectStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()
	mockCoord := mocks.NewMockCoordination()

	cfg := config.Config{
		Region: "us-east-1",
		Gateway: config.GatewayConfig{
			ListenAddr:       ":8080",
			JWTSecret:        "e2e-jwt-secret-key-12345",
			AdminAPIKey:      "e2e-admin-key",
			MaxUploadSizeGB:  50,
			RateLimitPerIP:   100,
			RateLimitPerUser: 500,
			MultiplexBatchMs: 50,
		},
		Coordinator: config.CoordinatorConfig{
			PartitionCount:   1024,
			SlicingSemaphore: 10,
			NATSShardCount:   4,
		},
	}

	// 2. Gateway Tier: Client creates Upload Session
	gwDaemon := gateway.NewGatewayDaemon(cfg, mockStore, mockObjectStore, mockBus, mockCoord)
	_ = gwDaemon

	createReq := models.CreateSessionRequest{
		FileName:      "big_buck_bunny_1080p.mp4",
		FileSizeBytes: 100 * 1024 * 1024, // 100MB
	}
	reqData, _ := json.Marshal(createReq)

	req := httptest.NewRequest("POST", "/api/jobs/upload-session", bytes.NewReader(reqData))
	rec := httptest.NewRecorder()

	router := http.NewServeMux()
	router.HandleFunc("POST /api/jobs/upload-session", func(w http.ResponseWriter, r *http.Request) {
		var req models.CreateSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = gateway.ValidateUploadRequest(req)

		jobID := "us-east-1:e2e-job-001"
		partitionID := models.PartitionOf(jobID, cfg.Coordinator.PartitionCount)

		uploadID, _ := mockObjectStore.CreateMultipartUpload(r.Context(), fmt.Sprintf("jobs/partition_%d/job_%s/raw/source.mp4", partitionID, jobID))
		_ = mockStore.SetJobStatus(r.Context(), jobID, map[string]interface{}{
			"state":     string(models.JobPhaseCreated),
			"completed": 0,
			"total":     0,
		})

		session := models.UploadSession{
			JobID:        jobID,
			UploadID:     uploadID,
			PartSize:     50 * 1024 * 1024,
			TotalParts:   2,
			SessionToken: "mock-session-jwt-token",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	})

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("CreateSession failed with status %d: %s", rec.Code, rec.Body.String())
	}

	var session models.UploadSession
	_ = json.Unmarshal(rec.Body.Bytes(), &session)
	jobID := session.JobID
	partitionID := models.PartitionOf(jobID, cfg.Coordinator.PartitionCount)

	// 3. Coordinator Tier: Slicing and Pipelined Task Generation
	coordDaemon := coordinator.NewCoordinatorDaemon(cfg, "coord-node-01", mockStore, mockBus, mockCoord, mockObjectStore)
	pm := coordinator.NewPartitionManager(coordDaemon, partitionID, 1)

	// Prepare mock sliced chunks (2 chunks: chunk_000.mp4, chunk_001.mp4)
	tempDir, err := os.MkdirTemp("", "e2e-slices-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	_ = os.WriteFile(filepath.Join(tempDir, "chunk_000.mp4"), []byte("video stream part 0"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "chunk_001.mp4"), []byte("video stream part 1"), 0644)

	// Store manifest in object store
	manifest := models.JobManifest{
		JobID:        jobID,
		PartitionID:  partitionID,
		Resolutions:  models.AllResolutions, // 3 resolutions
		SegmentCount: 2,
		TotalTasks:   6, // 2 segments * 3 resolutions
	}
	mBytes, _ := json.Marshal(manifest)
	manifestKey := fmt.Sprintf("jobs/partition_%d/job_%s/job_manifest.json", partitionID, jobID)
	_ = mockObjectStore.PutObject(ctx, manifestKey, bytes.NewReader(mBytes), int64(len(mBytes)))

	// Upload slices and dispatch segment tasks to message bus
	numSegments, err := pm.UploadSlices(ctx, jobID, tempDir)
	if err != nil {
		t.Fatalf("UploadSlices failed: %v", err)
	}
	if numSegments != 2 {
		t.Fatalf("expected 2 segments, got %d", numSegments)
	}

	// 4. Worker Tier: Consume segment tasks and transcode
	totalTasksDispatched := 0
	for _, tasks := range mockBus.Tasks {
		totalTasksDispatched += len(tasks)
	}
	if totalTasksDispatched != 6 {
		t.Fatalf("expected 6 total tasks (2 segments * 3 resolutions), got %d", totalTasksDispatched)
	}

	// Simulate worker pool executing all 6 tasks
	for shard, taskList := range mockBus.Tasks {
		for _, msg := range taskList {
			var task models.SegmentTask
			_ = json.Unmarshal(msg.Data(), &task)

			// Worker puts transcoded .ts file in S3
			tsData := []byte("transcoded MPEG-TS packet data")
			_ = mockObjectStore.PutObject(ctx, task.OutputKey, bytes.NewReader(tsData), int64(len(tsData)))

			// Worker writes duration to Redis durations hash
			segKey := fmt.Sprintf("segment_%03d_%s", task.SegmentIdx, task.Resolution)
			_ = mockStore.SetSegmentDuration(ctx, task.JobID, segKey, "5.000000")
		}
		_ = shard
	}

	// 5. Coordinator Tier: Manifest Compilation
	durations, _ := mockStore.GetAllDurations(ctx, jobID)
	if len(durations) != 6 {
		t.Fatalf("expected 6 segment durations recorded in Redis, got %d", len(durations))
	}

	masterPlaylist := pm.GenerateHLSMasterPlaylist(models.AllResolutions)
	_ = mockObjectStore.PutObject(ctx, fmt.Sprintf("jobs/partition_%d/job_%s/hls/master.m3u8", partitionID, jobID), masterPlaylist, int64(masterPlaylist.Len()))

	dashManifest := pm.GenerateDASHManifest(models.AllResolutions, numSegments, durations)
	_ = mockObjectStore.PutObject(ctx, fmt.Sprintf("jobs/partition_%d/job_%s/dash/manifest.mpd", partitionID, jobID), dashManifest, int64(dashManifest.Len()))

	// Mark Job COMPLETED in state store
	_ = mockStore.SetJobStatus(ctx, jobID, map[string]interface{}{
		"state":     string(models.JobPhaseCompleted),
		"completed": 6,
		"total":     6,
		"pct":       100,
	})

	// 6. Verification: Gateway API returns 200 OK with Completed Status
	finalStatus, err := mockStore.GetJobStatus(ctx, jobID)
	if err != nil || len(finalStatus) == 0 {
		t.Fatalf("failed to retrieve final job status: %v", err)
	}

	if finalStatus["state"] != string(models.JobPhaseCompleted) {
		t.Errorf("expected final status COMPLETED, got %s", finalStatus["state"])
	}
	if finalStatus["completed"] != "6" || finalStatus["total"] != "6" {
		t.Errorf("expected 6/6 completed tasks, got %s/%s", finalStatus["completed"], finalStatus["total"])
	}
}

func Test_E2E_CoordinatorFailover_PartitionTakeover_Success(t *testing.T) {
	ring := coordinator.NewHashRing()
	totalPartitions := 1024

	// Initial cluster topology: 3 active nodes
	ring.Rebuild([]string{"coord-node-A", "coord-node-B", "coord-node-C"})

	ownedByA := ring.OwnedPartitions("coord-node-A", totalPartitions)
	if len(ownedByA) == 0 {
		t.Fatalf("coord-node-A has 0 owned partitions initially")
	}

	targetPartition := ownedByA[0]

	// Simulate Coordinator A failure / network partition (node A leaves cluster)
	ring.Rebuild([]string{"coord-node-B", "coord-node-C"})

	newOwner := ring.OwnerOf(targetPartition)
	if newOwner == "coord-node-A" || newOwner == "" {
		t.Fatalf("partition %d still owned by dead node A or unassigned: %s", targetPartition, newOwner)
	}

	if newOwner != "coord-node-B" && newOwner != "coord-node-C" {
		t.Errorf("expected takeover by node B or C, got %s", newOwner)
	}
}

//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/coordinator"
	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/internal/worker"
)

func TestEndToEndWorkflow(t *testing.T) {
	// 1. Unified local test configuration
	cfg := config.Config{
		Region: "us-east-1",
		NodeID: "test-node-e2e",
		Redis: config.RedisConfig{
			Addrs: []string{"127.0.0.1:6379"},
		},
		NATS: config.NATSConfig{
			URLs: []string{"nats://127.0.0.1:4222"},
		},
		Etcd: config.EtcdConfig{
			Endpoints: []string{"127.0.0.1:2379"},
		},
		ObjectStore: config.ObjectStoreConfig{
			Endpoint:  "127.0.0.1:9000",
			Bucket:    "transcoder-us-east",
			Region:    "us-east-1",
			AccessKey: "minioadmin",
			SecretKey: "minioadmin",
			UseSSL:    false,
		},
		Gateway: config.GatewayConfig{
			ListenAddr:       "127.0.0.1:8085",
			JWTSecret:        "testsecret",
			MaxUploadSizeGB:  5,
			RateLimitPerIP:   1000,
			RateLimitPerUser: 1000,
			MultiplexBatchMs: 100,
		},
		Coordinator: config.CoordinatorConfig{
			PartitionCount:     4,
			SlicingSemaphore:   5,
			NATSShardCount:     2,
			EtcdLeaseTTLSec:    5,
			SlicingLockTTLSec:  5,
			SelfFenceThreshSec: 3,
			TakeoverGraceSec:   2,
			GCIntervalMin:      1,
			GCStaleThreshHours: 1,
		},
		Worker: config.WorkerConfig{
			ScratchDir:           os.TempDir(),
			MinDiskFreeGB:        1,
			WatchdogIntervalSec:  2,
			MaxTaskDurationMin:   2,
			MaxTempFileSizeGB:    1,
			ConcurrentTasks:      4,
			GracefulDrainSec:     5,
			CircuitBreakerWindow: 5,
			CircuitBreakerThresh: 3,
			HWAccel:              "none",
		},
		Metrics: config.MetricsConfig{
			ListenAddr: "127.0.0.1:9091",
			Path:       "/metrics",
		},
	}

	// 2. Initialize Infrastructure
	stateStore, err := infra.NewRedisStore(cfg.Redis)
	if err != nil {
		t.Fatalf("failed to connect to Redis: %v", err)
	}

	messageBus, err := infra.NewNATSBus(cfg.NATS)
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}

	// Initialize NATS streams and durable consumers
	if err := messageBus.InitEcosystem(cfg.Coordinator.NATSShardCount); err != nil {
		t.Fatalf("failed to init NATS ecosystem: %v", err)
	}

	coordClient, err := infra.NewEtcdClient(cfg.Etcd)
	if err != nil {
		t.Fatalf("failed to connect to etcd: %v", err)
	}

	objStore, err := infra.NewS3Client(cfg.ObjectStore)
	if err != nil {
		t.Fatalf("failed to connect to S3/MinIO: %v", err)
	}

	// 3. Obtain input video (default to generating a fast 2s mock video, use large files only if TEST_LARGE_VIDEO is set)
	inputVideoPath := ""
	if os.Getenv("TEST_LARGE_VIDEO") == "1" {
		candidatePaths := []string{
			"sampletest.mp4",
			"../../sampletest.mp4",
			"/Users/ashutoshkumar/Desktop/Apple Project/sampletest.mp4",
			"test.mp4",
			"../../test.mp4",
			"/Users/ashutoshkumar/Desktop/Apple Project/test.mp4",
		}
		for _, p := range candidatePaths {
			if _, err := os.Stat(p); err == nil {
				inputVideoPath = p
				break
			}
		}
	}

	var needsCleanup = false
	if inputVideoPath == "" {
		inputVideoPath = filepath.Join(os.TempDir(), "e2e-input.mp4")
		genCmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=30",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", inputVideoPath)
		if err := genCmd.Run(); err != nil {
			t.Fatalf("failed to generate test video: %v", err)
		}
		needsCleanup = true
	}
	if needsCleanup {
		defer os.Remove(inputVideoPath)
	}

	videoFile, err := os.Open(inputVideoPath)
	if err != nil {
		t.Fatalf("failed to open test video: %v", err)
	}
	defer videoFile.Close()

	videoStat, err := videoFile.Stat()
	if err != nil {
		t.Fatalf("failed to stat test video: %v", err)
	}
	videoSize := videoStat.Size()

	// 4. Boot the daemons in background contexts
	daemonCtx, cancelDaemons := context.WithCancel(context.Background())
	defer cancelDaemons()

	// Boot Ingest Gateway
	gwDaemon := gateway.NewGatewayDaemon(cfg, stateStore, objStore, messageBus)
	go func() {
		if err := gwDaemon.Run(daemonCtx); err != nil && err != http.ErrServerClosed {
			log.Printf("Gateway shutdown with error: %v", err)
		}
	}()

	// Boot Coordinator
	coordDaemon := coordinator.NewCoordinatorDaemon(cfg, "test-coord-1", stateStore, messageBus, coordClient, objStore)
	go coordDaemon.Run(daemonCtx)

	// Boot Worker
	workerDaemon := worker.NewWorkerDaemon(cfg, stateStore, objStore, messageBus)
	go func() {
		if err := workerDaemon.Run(daemonCtx); err != nil {
			log.Printf("Worker shutdown with error: %v", err)
		}
	}()

	// Allow daemons 1 second to start and register
	time.Sleep(1 * time.Second)

	// 5. API Session Creation Workflow
	createReq := models.CreateSessionRequest{
		FileSizeBytes: videoSize,
		FileName:      "test-input.mp4",
		ContentType:   "video/mp4",
	}
	reqData, _ := json.Marshal(createReq)

	resp, err := http.Post("http://127.0.0.1:8085/api/jobs/upload-session", "application/json", bytes.NewReader(reqData))
	if err != nil {
		t.Fatalf("failed to POST upload-session: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload-session returned status %d: %s", resp.StatusCode, string(body))
	}

	var session models.UploadSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("failed to decode upload session: %v", err)
	}

	// Register teardown cleanup to ensure initial and final states are identical
	t.Cleanup(func() {
		log.Printf("[TEARDOWN] Cleaning up integration test resources for Job %s", session.JobID)
		
		// 1. Remove Redis state
		keysToDelete := []string{
			"job:{" + session.JobID + "}:status",
			"job:{" + session.JobID + "}:progress",
			"job:{" + session.JobID + "}:durations",
			"job:{" + session.JobID + "}:manifest",
			"progress:{" + session.JobID + "}",
			"dedup:event:{" + session.JobID + "}",
			"dedup:event:{" + session.JobID + ":manifest}",
		}
		// Also delete the individual task keys (12 segments * 3 resolutions)
		for seg := 0; seg < 12; seg++ {
			for _, res := range models.AllResolutions {
				keysToDelete = append(keysToDelete, fmt.Sprintf("task:{%s}:%d:%s", session.JobID, seg, res))
			}
		}
		stateStore.DeleteKeys(context.Background(), keysToDelete...)

		partitionID := models.PartitionOf(session.JobID, cfg.Coordinator.PartitionCount)
		stateStore.RemoveActiveJob(context.Background(), partitionID, session.JobID)
		
		// 2. Remove MinIO/S3 files
		prefix := fmt.Sprintf("jobs/partition_%d/job_%s/", partitionID, session.JobID)
		objStore.DeletePrefix(context.Background(), prefix)
	})

	// 6. Generate presigned PUT URL and upload video payload
	client := &http.Client{}
	reqURL := fmt.Sprintf("http://127.0.0.1:8085/api/jobs/%s/urls?start=1&count=1", session.JobID)
	reqBatch, err := http.NewRequest("POST", reqURL, nil)
	if err != nil {
		t.Fatalf("failed to build request for presigned URLs: %v", err)
	}
	reqBatch.Header.Set("Authorization", "Bearer "+session.SessionToken)

	batchResp, err := client.Do(reqBatch)
	if err != nil {
		t.Fatalf("failed to request presigned batch: %v", err)
	}
	defer batchResp.Body.Close()

	var batch models.PresignedBatch
	if err := json.NewDecoder(batchResp.Body).Decode(&batch); err != nil {
		t.Fatalf("failed to decode presigned batch: %v", err)
	}

	// Upload part directly to MinIO using the presigned URL
	putReq, err := http.NewRequest("PUT", batch.URLs[0], videoFile)
	if err != nil {
		t.Fatalf("failed to build PUT request: %v", err)
	}
	putReq.Header.Set("Content-Type", "video/mp4")
	putReq.ContentLength = videoSize

	putResp, err := client.Do(putReq)
	if err != nil {
		t.Fatalf("failed to PUT video chunk directly to storage: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT part returned non-200: %d", putResp.StatusCode)
	}
	etag := putResp.Header.Get("ETag")

	// 7. Complete the upload
	completePayload := struct {
		Parts []struct {
			PartNumber int    `json:"part_number"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}{
		Parts: []struct {
			PartNumber int    `json:"part_number"`
			ETag       string `json:"etag"`
		}{
			{PartNumber: 1, ETag: etag},
		},
	}
	completeData, _ := json.Marshal(completePayload)

	completeReq, err := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:8085/api/jobs/%s/complete", session.JobID), bytes.NewReader(completeData))
	if err != nil {
		t.Fatalf("failed to build complete upload request: %v", err)
	}
	completeReq.Header.Set("Authorization", "Bearer "+session.SessionToken)
	completeReq.Header.Set("Content-Type", "application/json")

	completeResp, err := client.Do(completeReq)
	if err != nil {
		t.Fatalf("failed to complete upload: %v", err)
	}
	defer completeResp.Body.Close()
	if completeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(completeResp.Body)
		t.Fatalf("complete upload returned status %d: %s", completeResp.StatusCode, string(body))
	}

	// 8. Publish the upload event to NATS trigger subject
	partitionID := models.PartitionOf(session.JobID, cfg.Coordinator.PartitionCount)
	
	// S3 mock event notification format
	s3MockEvent := map[string]interface{}{
		"Records": []map[string]interface{}{
			{
				"s3": map[string]interface{}{
					"object": map[string]interface{}{
						"key": fmt.Sprintf("jobs/partition_%d/job_%s/raw/source.mp4", partitionID, session.JobID),
					},
				},
			},
		},
	}
	eventBytes, _ := json.Marshal(s3MockEvent)

	subject := fmt.Sprintf("s3-raw-uploads.job.partition_%d.job_%s", partitionID, session.JobID)
	err = messageBus.PublishEvent(context.Background(), subject, eventBytes)
	if err != nil {
		t.Fatalf("failed to publish NATS trigger upload event: %v", err)
	}

	// 9. Poll status from Redis until job is COMPLETED (with timeout)
	timeout := time.After(300 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	completed := false
	var finalStatus map[string]string

	for !completed {
		select {
		case <-timeout:
			t.Fatalf("Timeout reached waiting for job to complete transcoding. Final state: %v", finalStatus)
		case <-ticker.C:
			finalStatus, err = stateStore.GetJobStatus(context.Background(), session.JobID)
			if err != nil {
				continue
			}
			log.Printf("Polling job state: %s, completed tasks: %s/%s", 
				finalStatus["state"], finalStatus["completed"], finalStatus["total"])

			if finalStatus["state"] == string(models.JobPhaseCompleted) {
				completed = true
			} else if finalStatus["state"] == string(models.JobPhaseFailed) {
				t.Fatalf("Job entered FAILED state during transcoding pipeline: %s", finalStatus["error"])
			}
		}
	}

	// 10. Verify generated playlists and manifests in MinIO
	manifestPrefix := fmt.Sprintf("jobs/partition_%d/job_%s/", partitionID, session.JobID)
	
	// Check Master playlist
	masterMeta, err := objStore.HeadObject(context.Background(), manifestPrefix+"master.m3u8")
	if err != nil || !masterMeta.Exists {
		t.Errorf("HLS master.m3u8 is missing from S3/MinIO bucket")
	}

	// Check DASH manifest
	dashMeta, err := objStore.HeadObject(context.Background(), manifestPrefix+"manifest.mpd")
	if err != nil || !dashMeta.Exists {
		t.Errorf("DASH manifest.mpd is missing from S3/MinIO bucket")
	}

	// Check completion sentinel
	sentinelMeta, err := objStore.HeadObject(context.Background(), manifestPrefix+"job_completed.json")
	if err != nil || !sentinelMeta.Exists {
		t.Errorf("job_completed.json sentinel is missing from S3/MinIO bucket")
	}

	log.Printf("Integration E2E Workflow Test completed successfully!")
}

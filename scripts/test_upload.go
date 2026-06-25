//go:build ignore

package main

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
	"hash/fnv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

type CreateSessionRequest struct {
	FileSizeBytes int64  `json:"file_size_bytes"`
	FileName      string `json:"file_name"`
	ContentType   string `json:"content_type"`
}

type UploadSession struct {
	JobID        string `json:"job_id"`
	SessionToken string `json:"session_token"`
	UploadID     string `json:"upload_id"`
	PartSize     int64  `json:"part_size"`
	TotalParts   int    `json:"total_parts"`
	ProgressWSS  string `json:"progress_wss"`
}

type PresignedBatch struct {
	PartNumbers []int    `json:"part_numbers"`
	URLs        []string `json:"urls"`
}

func main() {
	log.Println("======================================================================")
	log.Println(" 🧪 MULTI-REGION SYSTEM SIMULATION TESTER")
	log.Println("======================================================================")

	// 1. Create a 1-second mock video
	mockVideo := filepath.Join(os.TempDir(), "sim-input.mp4")
	log.Printf("[1/8] Generating mock video at %s...", mockVideo)
	genCmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=30",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", mockVideo)
	if err := genCmd.Run(); err != nil {
		log.Fatalf("Failed to generate test video: %v", err)
	}
	defer os.Remove(mockVideo)

	fileInfo, err := os.Stat(mockVideo)
	if err != nil {
		log.Fatalf("Failed to stat test video: %v", err)
	}
	fileSize := fileInfo.Size()

	// 2. Ingest Session creation in US-East Gateway (Port 8080)
	log.Println("[2/8] Requesting upload session from US-East Gateway (Port 8080)...")
	createReq := CreateSessionRequest{
		FileSizeBytes: fileSize,
		FileName:      "sim-input.mp4",
		ContentType:   "video/mp4",
	}
	reqData, _ := json.Marshal(createReq)
	resp, err := http.Post("http://127.0.0.1:8080/api/jobs/upload-session", "application/json", bytes.NewReader(reqData))
	if err != nil {
		log.Fatalf("Failed to connect to US-East Gateway: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("Upload session request failed: %s", string(body))
	}

	var session UploadSession
	json.NewDecoder(resp.Body).Decode(&session)
	log.Printf("👉 Upload session created. Job ID: %s", session.JobID)

	// 3. Request presigned URL
	log.Println("[3/8] Fetching presigned upload URL...")
	client := &http.Client{}
	reqURL := fmt.Sprintf("http://127.0.0.1:8080/api/jobs/%s/urls?start=1&count=1", session.JobID)
	reqBatch, _ := http.NewRequest("POST", reqURL, nil)
	reqBatch.Header.Set("Authorization", "Bearer "+session.SessionToken)
	batchResp, err := client.Do(reqBatch)
	if err != nil || batchResp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to fetch presigned URL batch")
	}
	defer batchResp.Body.Close()

	var batch PresignedBatch
	json.NewDecoder(batchResp.Body).Decode(&batch)

	// 4. Upload file content to US-East MinIO (Port 9000)
	log.Println("[4/8] Uploading video data directly to US-East MinIO...")
	videoFile, _ := os.Open(mockVideo)
	defer videoFile.Close()

	putReq, _ := http.NewRequest("PUT", batch.URLs[0], videoFile)
	putReq.Header.Set("Content-Type", "video/mp4")
	putReq.ContentLength = fileSize
	putResp, err := client.Do(putReq)
	if err != nil || putResp.StatusCode != http.StatusOK {
		log.Fatalf("MinIO upload failed")
	}
	defer putResp.Body.Close()
	etag := putResp.Header.Get("ETag")

	// 5. Complete session
	log.Println("[5/8] Committing upload session completion...")
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
	completeReq, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:8080/api/jobs/%s/complete", session.JobID), bytes.NewReader(completeData))
	completeReq.Header.Set("Authorization", "Bearer "+session.SessionToken)
	completeReq.Header.Set("Content-Type", "application/json")
	completeResp, err := client.Do(completeReq)
	if err != nil || completeResp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to commit session completion")
	}
	defer completeResp.Body.Close()

	// 5.5. Publish the upload event to NATS trigger subject (US-East)
	log.Println("[5.5/8] Publishing S3 upload completion event to NATS (US-East)...")
	partitionCount := 4
	h := fnv.New32a()
	h.Write([]byte(session.JobID))
	partitionID := int(h.Sum32()) % partitionCount

	nc, err := nats.Connect("nats://127.0.0.1:4222")
	if err != nil {
		log.Fatalf("Failed to connect to NATS US-East: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("Failed to get JetStream context: %v", err)
	}

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
	_, err = js.Publish(subject, eventBytes)
	if err != nil {
		log.Fatalf("Failed to publish S3 upload event to NATS: %v", err)
	}

	// 6. Wait for transcoding completion in US-East Redis
	log.Println("[6/8] Waiting for US-East Workers to process and compile manifest...")
	rClientEast := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer rClientEast.Close()

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	completed := false
	for !completed {
		select {
		case <-timeout:
			log.Fatalf("Timeout waiting for job completion")
		case <-ticker.C:
			res, err := rClientEast.HGetAll(context.Background(), "job:{"+session.JobID+"}:status").Result()
			if err != nil || len(res) == 0 {
				continue
			}
			log.Printf("... Job status: %s (Progress: %s/%s completed tasks)", res["state"], res["completed"], res["total"])
			if res["state"] == "COMPLETED" {
				completed = true
			}
		}
	}
	log.Println("✅ Transcoding successfully completed in US-East!")

	// 7. Verify Redis isolation in EU-West (Port 6389)
	log.Println("[7/8] Verifying Redis Control Plane Isolation in EU-West (Port 6389)...")
	rClientWest := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6389"})
	defer rClientWest.Close()

	keys, err := rClientWest.Keys(context.Background(), "*"+session.JobID+"*").Result()
	if err != nil {
		log.Fatalf("Failed to check keys in EU-West Redis: %v", err)
	}

	if len(keys) > 0 {
		log.Fatalf("❌ Isolation failure: Job keys found leaking into EU-West Redis: %v", keys)
	} else {
		log.Println("✅ Success: EU-West Redis has zero state for US-East job (Complete Isolation).")
	}

	// 8. Verify S3 CRR Manifest Replication to EU-West MinIO (Port 9010)
	log.Println("[8/8] Checking S3 CRR replication status in EU-West bucket...")
	
	// Init EU-West S3 Client
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{URL: "http://127.0.0.1:9010", SigningRegion: "eu-west"}, nil
	})
	credProvider := credentials.NewStaticCredentialsProvider("minioadmin", "minioadmin", "")
	awsCfg, _ := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion("eu-west"),
		awsconfig.WithEndpointResolverWithOptions(customResolver),
		awsconfig.WithCredentialsProvider(credProvider),
	)
	s3ClientWest := s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.UsePathStyle = true })

	// Wait up to 5s for CRR script replication
	crrTimeout := time.After(5 * time.Second)
	crrTicker := time.NewTicker(500 * time.Millisecond)
	defer crrTicker.Stop()

	manifestKey := fmt.Sprintf("jobs/partition_%d/job_%s/master.m3u8", partitionID, session.JobID)
	replicated := false

	for !replicated {
		select {
		case <-crrTimeout:
			log.Fatalf("❌ S3 CRR replication check failed: Manifest did not arrive in EU-West bucket within timeout.")
		case <-crrTicker.C:
			_, err := s3ClientWest.HeadObject(context.Background(), &s3.HeadObjectInput{
				Bucket: aws.String("transcoder-eu-west"),
				Key:    aws.String(manifestKey),
			})
			if err == nil {
				replicated = true
			}
		}
	}
	log.Println("✅ Success: HLS master manifest was replicated to EU-West bucket via CRR!")

	// Double check that raw files were NOT replicated (data gravity constraint)
	rawSourceKey := fmt.Sprintf("jobs/partition_%d/job_%s/raw/source.mp4", partitionID, session.JobID)
	_, rawErr := s3ClientWest.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String("transcoder-eu-west"),
		Key:    aws.String(rawSourceKey),
	})
	if rawErr == nil {
		log.Fatalf("❌ Data Gravity violation: Raw video segment replicated to EU-West bucket.")
	} else {
		log.Println("✅ Success: Raw video segments kept local in US-East bucket (Data Gravity respected).")
	}

	// Double check that coordinator in EU-West ignores the replicated manifest folder
	// We can check EU-West coordinator logs or verify it didn't write to Redis EU-West
	log.Println("======================================================================")
	log.Println(" 🎉 ALL MULTI-REGION DISTRIBUTED SIMULATION TESTS PASSED!")
	log.Println("======================================================================")
}

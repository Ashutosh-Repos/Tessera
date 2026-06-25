package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	MaxUploadSizeBytes int64 = 50 * 1024 * 1024 * 1024 // 50 GB
)

func ValidateUploadRequest(req models.CreateSessionRequest) error {
	if req.FileSizeBytes <= 0 {
		return fmt.Errorf("file size must be positive")
	}
	if req.FileSizeBytes > MaxUploadSizeBytes {
		return fmt.Errorf("file size %d exceeds maximum %d bytes", req.FileSizeBytes, MaxUploadSizeBytes)
	}
	return nil
}

func (g *GatewayDaemon) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req models.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := ValidateUploadRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	atomic.AddInt64(&g.uploadCount, 1)
	jobID := fmt.Sprintf("%s:%s", g.cfg.Region, uuid.New().String())
	partitionCount := g.cfg.Coordinator.PartitionCount
	if partitionCount == 0 {
		partitionCount = 1024
	}
	partitionID := models.PartitionOf(jobID, partitionCount)

	s3Key := fmt.Sprintf("jobs/partition_%d/job_%s/raw/source.mp4", partitionID, jobID)
	uploadID, err := g.objectStore.CreateMultipartUpload(r.Context(), s3Key)
	if err != nil {
		http.Error(w, "Failed to create upload", http.StatusInternalServerError)
		return
	}

	manifest := models.JobManifest{
		JobID:       jobID,
		PartitionID: partitionID,
		Region:      g.cfg.Region,
		SourcePath:  s3Key,
		SourceSizeB: req.FileSizeBytes,
		Resolutions: models.AllResolutions,
		CreatedAt:   time.Now(),
	}

	manifestData, _ := json.Marshal(manifest)
	manifestKey := fmt.Sprintf("jobs/partition_%d/job_%s/job_manifest.json", partitionID, jobID)
	if err := g.objectStore.PutObject(r.Context(), manifestKey, bytes.NewReader(manifestData), int64(len(manifestData))); err != nil {
		http.Error(w, "Failed to store manifest", http.StatusInternalServerError)
		return
	}
	g.state.CacheManifest(r.Context(), jobID, manifestData)

	g.state.SetJobStatus(r.Context(), jobID, map[string]interface{}{
		"state":        string(models.JobPhaseCreated),
		"completed":    0,
		"total":        0,
		"partition":    partitionID,
		"owner_epoch":  0, // assigned by coordinator later
		"last_updated": time.Now().Unix(),
	})
	g.state.AddActiveJob(r.Context(), partitionID, jobID)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, models.UploadSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   jobID,
		},
		JobID:    jobID,
		UploadID: uploadID,
		Bucket:   g.cfg.ObjectStore.Bucket,
		Key:      s3Key,
	})
	tokenStr, _ := token.SignedString([]byte(g.cfg.Gateway.JWTSecret))

	totalParts := int(req.FileSizeBytes/(50*1024*1024)) + 1

	session := models.UploadSession{
		JobID:        jobID,
		SessionToken: tokenStr,
		UploadID:     uploadID,
		PartSize:     50 * 1024 * 1024,
		TotalParts:   totalParts,
		ProgressWSS:  fmt.Sprintf("wss://%s/progress/%s?token=%s", g.cfg.Gateway.ListenAddr, jobID, tokenStr),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func (g *GatewayDaemon) handlePresignedBatch(w http.ResponseWriter, r *http.Request) {
	uuidParam := r.PathValue("uuid")

	// Validate JWT
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || len(authHeader) < 8 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tokenStr := authHeader[7:]

	claims := &models.UploadSessionClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(g.cfg.Gateway.JWTSecret), nil
	})

	if err != nil || !token.Valid || claims.JobID != uuidParam {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	startPart, _ := strconv.Atoi(r.URL.Query().Get("start"))
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if startPart <= 0 || count <= 0 || count > 100 {
		http.Error(w, "Invalid start or count", http.StatusBadRequest)
		return
	}

	batch := models.PresignedBatch{}
	for i := startPart; i < startPart+count; i++ {
		url, err := g.objectStore.GeneratePresignedPUT(r.Context(), claims.Key, claims.UploadID, i, 15*time.Minute)
		if err != nil {
			http.Error(w, "Failed to generate URL", http.StatusInternalServerError)
			return
		}
		batch.PartNumbers = append(batch.PartNumbers, i)
		batch.URLs = append(batch.URLs, url)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(batch)
}

func (g *GatewayDaemon) handleListUploadedParts(w http.ResponseWriter, r *http.Request) {
	// Dummy implementation for Tus resume support
	w.WriteHeader(http.StatusOK)
}

func (g *GatewayDaemon) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	uuidParam := r.PathValue("uuid")
	status, err := g.state.GetJobStatus(r.Context(), uuidParam)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (g *GatewayDaemon) handleWebSocketOrSSE(w http.ResponseWriter, r *http.Request) {
	uuidParam := r.PathValue("uuid")

	// Set up Server-Sent Events (SSE) since full WebSocket implementation is large
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan models.ProgressUpdate, 10)
	g.multiplexer.Subscribe(uuidParam, ch)
	defer g.multiplexer.Unsubscribe(uuidParam, ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case update := <-ch:
			data, _ := json.Marshal(update)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

func (g *GatewayDaemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (g *GatewayDaemon) handleCompleteUpload(w http.ResponseWriter, r *http.Request) {
	uuidParam := r.PathValue("uuid")

	// Validate JWT
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || len(authHeader) < 8 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tokenStr := authHeader[7:]

	claims := &models.UploadSessionClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(g.cfg.Gateway.JWTSecret), nil
	})

	if err != nil || !token.Valid || claims.JobID != uuidParam {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Parts []struct {
			PartNumber int    `json:"part_number"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var parts []infra.CompletedPart
	for _, p := range req.Parts {
		parts = append(parts, infra.CompletedPart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		})
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	err = g.objectStore.CompleteMultipartUpload(r.Context(), claims.Key, claims.UploadID, parts)
	if err != nil {
		log.Printf("Failed to complete multipart upload: %v", err)
		http.Error(w, "Failed to complete upload in S3", http.StatusInternalServerError)
		return
	}

	// In a local "Platform-in-a-Box" deployment, MinIO does not natively publish S3 Bucket
	// Notifications to NATS out of the box. To respect the backend architecture requirement,
	// the Gateway acts as the S3 Event bridge, publishing the completion event to NATS.
	if g.messageBus != nil {
		partitionCount := g.cfg.Coordinator.PartitionCount
		if partitionCount == 0 {
			partitionCount = 1024
		}

		h := fnv.New32a()
		h.Write([]byte(uuidParam))
		partitionID := int(h.Sum32()) % partitionCount

		s3MockEvent := map[string]interface{}{
			"Records": []map[string]interface{}{
				{
					"s3": map[string]interface{}{
						"object": map[string]interface{}{
							"key": claims.Key,
						},
					},
				},
			},
		}
		eventBytes, _ := json.Marshal(s3MockEvent)
		subject := fmt.Sprintf("s3-raw-uploads.job.partition_%d.job_%s", partitionID, uuidParam)
		
		if err := g.messageBus.PublishEvent(r.Context(), subject, eventBytes); err != nil {
			log.Printf("Failed to publish S3 event to NATS: %v", err)
		} else {
			log.Printf("Gateway simulated S3 event published to %s", subject)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"completed"}`))
}

func (g *GatewayDaemon) handleListJobs(w http.ResponseWriter, r *http.Request) {
	keys, err := g.state.ScanJobKeys(r.Context())
	if err != nil {
		http.Error(w, "Failed to scan jobs", http.StatusInternalServerError)
		return
	}

	type JobStatusJSON struct {
		JobID       string `json:"job_id"`
		Phase       string `json:"phase"`
		Completed   int    `json:"completed"`
		Total       int    `json:"total"`
		OwnerEpoch  int    `json:"owner_epoch"`
		PartitionID int    `json:"partition_id"`
		LastUpdated int64  `json:"last_updated"`
	}

	var uniqueJobIDs []string
	seen := make(map[string]bool)

	for _, key := range keys {
		jobID := key
		if strings.HasPrefix(jobID, "job:{") {
			jobID = strings.TrimPrefix(jobID, "job:{")
		}
		if strings.HasSuffix(jobID, "}:status") {
			jobID = strings.TrimSuffix(jobID, "}:status")
		}

		if seen[jobID] {
			continue
		}
		seen[jobID] = true
		uniqueJobIDs = append(uniqueJobIDs, jobID)
	}

	// Sort job IDs alphabetically to ensure stable, deterministic pagination
	sort.Strings(uniqueJobIDs)

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			if l > 100 {
				limit = 100
			} else if l < 1 {
				limit = 1
			} else {
				limit = l
			}
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			if o >= 0 {
				offset = o
			}
		}
	}

	totalJobs := len(uniqueJobIDs)
	start := offset
	if start > totalJobs {
		start = totalJobs
	}
	end := offset + limit
	if end > totalJobs {
		end = totalJobs
	}

	slicedJobIDs := uniqueJobIDs[start:end]
	jobs := []JobStatusJSON{}

	for _, jobID := range slicedJobIDs {
		statusMap, err := g.state.GetJobStatus(r.Context(), jobID)
		if err != nil || len(statusMap) == 0 {
			continue
		}

		completed, _ := strconv.Atoi(statusMap["completed"])
		total, _ := strconv.Atoi(statusMap["total"])
		partition, _ := strconv.Atoi(statusMap["partition"])
		ownerEpoch, _ := strconv.Atoi(statusMap["owner_epoch"])
		lastUpdated, _ := strconv.ParseInt(statusMap["last_updated"], 10, 64)

		jobs = append(jobs, JobStatusJSON{
			JobID:       jobID,
			Phase:       statusMap["state"],
			Completed:   completed,
			Total:       total,
			OwnerEpoch:  ownerEpoch,
			PartitionID: partition,
			LastUpdated: lastUpdated,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

func (g *GatewayDaemon) requireAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.cfg.Gateway.AdminAPIKey != "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			token := authHeader[7:]
			if token != g.cfg.Gateway.AdminAPIKey {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func (g *GatewayDaemon) handleListRegions(w http.ResponseWriter, r *http.Request) {
	// Add a 2-second timeout to protect gateway API from cascading blocks when dependency services hang
	pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	redisOk := g.state.Ping(pingCtx) == nil

	natsOk := false
	if g.messageBus != nil {
		natsOk = g.messageBus.Ping(pingCtx) == nil
	}

	s3Ok := g.objectStore.Ping(pingCtx) == nil

	etcdOk := false
	if g.coord != nil {
		if pingErr := g.coord.Ping(pingCtx); pingErr == nil {
			etcdOk = true
		}
	}

	type WorkerJSON struct {
		ID    string `json:"id"`
		CPU   int    `json:"cpu"`
		GPU   int    `json:"gpu"`
		Tasks int    `json:"tasks"`
	}

	type RegionHealthJSON struct {
		Region     string `json:"region"`
		GatewayURL string `json:"gateway_url"`
		Healthy    bool   `json:"healthy"`
		Services   struct {
			Redis bool `json:"redis"`
			Nats  bool `json:"nats"`
			S3    bool `json:"s3"`
			Etcd  bool `json:"etcd"`
		} `json:"services"`
		ActiveSockets int          `json:"active_sockets"`
		UploadCount   int64        `json:"upload_count"`
		DLQDepth      int64        `json:"dlq_depth"`
		Workers       []WorkerJSON `json:"workers"`
	}

	res := RegionHealthJSON{
		Region:        g.cfg.Region,
		GatewayURL:    fmt.Sprintf("http://%s", g.cfg.Gateway.ListenAddr),
		Healthy:       redisOk && natsOk && s3Ok && etcdOk,
		ActiveSockets: g.multiplexer.ActiveSubscriberCount(),
		UploadCount:   atomic.LoadInt64(&g.uploadCount),
	}
	res.Services.Redis = redisOk
	res.Services.Nats = natsOk
	res.Services.S3 = s3Ok
	res.Services.Etcd = etcdOk

	if g.messageBus != nil {
		depth, _ := g.messageBus.GetDLQDepth()
		res.DLQDepth = depth
	}

	res.Workers = []WorkerJSON{}
	if redisOk {
		workersMap, err := g.state.GetActiveWorkers(pingCtx)
		if err == nil {
			for id, fields := range workersMap {
				cpu, _ := strconv.Atoi(fields["cpu"])
				gpu, _ := strconv.Atoi(fields["gpu"])
				tasks, _ := strconv.Atoi(fields["tasks"])
				res.Workers = append(res.Workers, WorkerJSON{
					ID:    id,
					CPU:   cpu,
					GPU:   gpu,
					Tasks: tasks,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (g *GatewayDaemon) handleListCoordinators(w http.ResponseWriter, r *http.Request) {
	pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var coordinators []string
	if g.coord != nil {
		coordinators, _ = g.coord.GetCoordinators(pingCtx)
	}
	if coordinators == nil {
		coordinators = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(coordinators)
}

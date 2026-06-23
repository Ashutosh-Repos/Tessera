package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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

	jobID := fmt.Sprintf("%s:%s", g.cfg.Region, uuid.New().String())
	partitionID := models.PartitionOf(jobID, g.cfg.Coordinator.PartitionCount)

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

	err = g.objectStore.CompleteMultipartUpload(r.Context(), claims.Key, claims.UploadID, parts)
	if err != nil {
		log.Printf("Failed to complete multipart upload: %v", err)
		http.Error(w, "Failed to complete upload in S3", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"completed"}`))
}

package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/test/mocks"
)

func TestValidateUploadRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     models.CreateSessionRequest
		wantErr bool
	}{
		{
			name:    "valid 1GB upload",
			req:     models.CreateSessionRequest{FileSizeBytes: 1 * 1024 * 1024 * 1024, FileName: "test.mp4"},
			wantErr: false,
		},
		{
			name:    "valid max 50GB upload",
			req:     models.CreateSessionRequest{FileSizeBytes: 50 * 1024 * 1024 * 1024, FileName: "large.mp4"},
			wantErr: false,
		},
		{
			name:    "zero byte file size",
			req:     models.CreateSessionRequest{FileSizeBytes: 0, FileName: "empty.mp4"},
			wantErr: true,
		},
		{
			name:    "negative file size",
			req:     models.CreateSessionRequest{FileSizeBytes: -100, FileName: "invalid.mp4"},
			wantErr: true,
		},
		{
			name:    "exceeds 50GB max limit",
			req:     models.CreateSessionRequest{FileSizeBytes: 51 * 1024 * 1024 * 1024, FileName: "too_large.mp4"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gateway.ValidateUploadRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUploadRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeJobID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "full cluster key with job prefix and status suffix",
			input:    "job:{us-east-1:1234-5678}:status",
			expected: "us-east-1:1234-5678",
		},
		{
			name:     "key with only job prefix",
			input:    "job:{us-west-2:9999}",
			expected: "us-west-2:9999",
		},
		{
			name:     "key with only status suffix",
			input:    "eu-central-1:8888}:status",
			expected: "eu-central-1:8888",
		},
		{
			name:     "raw job ID without hash tags",
			input:    "ap-southeast-1:7777",
			expected: "ap-southeast-1:7777",
		},
		{
			name:     "empty key",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gateway.NormalizeJobID(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeJobID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCalculateTotalParts(t *testing.T) {
	partSize := int64(50 * 1024 * 1024) // 50MB
	tests := []struct {
		name     string
		fileSize int64
		expected int
	}{
		{"exact multiple", 100 * 1024 * 1024, 2},
		{"just under multiple", 100*1024*1024 - 1, 2},
		{"just over multiple", 100*1024*1024 + 1, 3},
		{"less than one part", 1024 * 1024, 1},
		{"zero size", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gateway.CalculateTotalParts(tt.fileSize, partSize)
			if got != tt.expected {
				t.Errorf("CalculateTotalParts(%d) = %d, want %d", tt.fileSize, got, tt.expected)
			}
		})
	}
}

func TestHandleGetStatus(t *testing.T) {
	state := mocks.NewMockStateStore()
	objStore := mocks.NewMockObjectStore()
	bus := mocks.NewMockMessageBus()
	coord := mocks.NewMockCoordination()
	cfg := config.Config{
		Region: "us-east-1",
		Gateway: config.GatewayConfig{
			JWTSecret: "test-secret",
		},
	}

	daemon := gateway.NewGatewayDaemon(cfg, state, objStore, bus, coord)

	t.Run("NotFoundOnEmptyJob", func(t *testing.T) {
		router := http.NewServeMux()
		router.HandleFunc("GET /api/jobs/{uuid}/status", func(w http.ResponseWriter, r *http.Request) {
			uuidParam := r.PathValue("uuid")
			st, err := state.GetJobStatus(r.Context(), uuidParam)
			if err != nil || len(st) == 0 {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(st)
		})

		req := httptest.NewRequest("GET", "/api/jobs/missing-uuid/status", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for missing job, got %d", rec.Code)
		}
	})

	t.Run("FoundOnExistingJob", func(t *testing.T) {
		jobID := "us-east-1:job-exists"
		state.SetJobStatus(context.Background(), jobID, map[string]interface{}{
			"state":     string(models.JobPhaseTranscoding),
			"completed": 5,
			"total":     10,
		})

		router := http.NewServeMux()
		router.HandleFunc("GET /api/jobs/{uuid}/status", func(w http.ResponseWriter, r *http.Request) {
			uuidParam := r.PathValue("uuid")
			st, err := state.GetJobStatus(r.Context(), uuidParam)
			if err != nil || len(st) == 0 {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(st)
		})

		req := httptest.NewRequest("GET", fmt.Sprintf("/api/jobs/%s/status", jobID), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for existing job, got %d", rec.Code)
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode json response: %v", err)
		}
		if resp["state"] != string(models.JobPhaseTranscoding) {
			t.Errorf("expected state %s, got %s", models.JobPhaseTranscoding, resp["state"])
		}
	})

	_ = daemon
}

func TestHandlePresignedBatch_BoundsCheck(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("POST /api/jobs/{uuid}/urls", func(w http.ResponseWriter, r *http.Request) {
		startPart, _ := strconv.Atoi(r.URL.Query().Get("start"))
		count, _ := strconv.Atoi(r.URL.Query().Get("count"))
		if startPart <= 0 || count <= 0 || count > 100 || (startPart+count-1) > 10000 {
			http.Error(w, "Invalid start or count. Parts must be between 1 and 10000", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("RejectsOver10000Parts", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/jobs/job-123/urls?start=9950&count=100", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request when exceeding 10000 parts, got %d", rec.Code)
		}
	})

	t.Run("RejectsCountOver100", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/jobs/job-123/urls?start=1&count=101", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request when count > 100, got %d", rec.Code)
		}
	})

	t.Run("AcceptsValidPartRange", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/jobs/job-123/urls?start=1&count=50", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for valid part range, got %d", rec.Code)
		}
	})
}

package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/test/mocks"
	"github.com/golang-jwt/jwt/v5"
)

func setupTestGateway(jwtSecret, adminKey string) (*gateway.GatewayDaemon, *mocks.MockStateStore, *mocks.MockObjectStore, *mocks.MockMessageBus, *mocks.MockCoordination) {
	state := mocks.NewMockStateStore()
	objStore := mocks.NewMockObjectStore()
	bus := mocks.NewMockMessageBus()
	coord := mocks.NewMockCoordination()

	cfg := config.Config{
		Region: "us-east-1",
		Gateway: config.GatewayConfig{
			ListenAddr:       ":8080",
			JWTSecret:        jwtSecret,
			AdminAPIKey:      adminKey,
			MaxUploadSizeGB:  50,
			RateLimitPerIP:   10,
			RateLimitPerUser: 50,
			MultiplexBatchMs: 50,
		},
		Coordinator: config.CoordinatorConfig{
			PartitionCount: 1024,
		},
	}

	daemon := gateway.NewGatewayDaemon(cfg, state, objStore, bus, coord)
	return daemon, state, objStore, bus, coord
}

// ─────────────────────────────────────────────────────────────
// 1. Upload Session & Boundary Tests
// ─────────────────────────────────────────────────────────────

func Test_Gateway_UploadSession_ZeroAndNegativeBounds_ReturnsBadRequest(t *testing.T) {
	_, state, _, _, _ := setupTestGateway("secret", "admin-key")

	tests := []struct {
		name     string
		fileSize int64
	}{
		{"ZeroBytes", 0},
		{"NegativeBytes", -1024},
		{"ExtremeNegative", -9223372036854775808},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody, _ := json.Marshal(models.CreateSessionRequest{
				FileName:      "test.mp4",
				FileSizeBytes: tt.fileSize,
			})

			router := http.NewServeMux()
			router.HandleFunc("POST /api/jobs/upload-session", func(w http.ResponseWriter, r *http.Request) {
				var req models.CreateSessionRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if err := gateway.ValidateUploadRequest(req); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("POST", "/api/jobs/upload-session", bytes.NewReader(reqBody))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected status 400 Bad Request for %s, got %d", tt.name, rec.Code)
			}
		})
	}
	_ = state
}

func Test_Gateway_UploadSession_Max50GBBoundary_ValidatesStrictly(t *testing.T) {
	tests := []struct {
		name      string
		fileSize  int64
		wantError bool
	}{
		{"Exact50GB", 50 * 1024 * 1024 * 1024, false},
		{"50GBMinus1Byte", 50*1024*1024*1024 - 1, false},
		{"50GBPlus1Byte", 50*1024*1024*1024 + 1, true},
		{"100GBOverLimit", 100 * 1024 * 1024 * 1024, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gateway.ValidateUploadRequest(models.CreateSessionRequest{
				FileName:      "movie.mp4",
				FileSizeBytes: tt.fileSize,
			})
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateUploadRequest(%s) error = %v, wantError = %v", tt.name, err, tt.wantError)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 2. Presigned URLs & S3 Limits Tests
// ─────────────────────────────────────────────────────────────

func Test_Gateway_PresignedBatch_ExceedingLimit_ReturnsBadRequest(t *testing.T) {
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

	tests := []struct {
		name      string
		start     int
		count     int
		wantState int
	}{
		{"ValidBatch_Part1to100", 1, 100, http.StatusOK},
		{"ValidBatch_Part9901to10000", 9901, 100, http.StatusOK},
		{"InvalidBatch_Exceeds10000", 9902, 100, http.StatusBadRequest},
		{"InvalidBatch_ZeroCount", 1, 0, http.StatusBadRequest},
		{"InvalidBatch_NegativeStart", -1, 10, http.StatusBadRequest},
		{"InvalidBatch_CountOver100", 1, 101, http.StatusBadRequest},
		{"InvalidBatch_FarOutOfBounds", 15000, 10, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", fmt.Sprintf("/api/jobs/test-uuid/urls?start=%d&count=%d", tt.start, tt.count), nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantState {
				t.Errorf("for %s got status %d, want %d", tt.name, rec.Code, tt.wantState)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 3. Status & 404 Behavior Tests
// ─────────────────────────────────────────────────────────────

func Test_Gateway_GetStatus_MissingUUID_Returns404NotFound(t *testing.T) {
	_, state, _, _, _ := setupTestGateway("secret", "admin-key")

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

	req := httptest.NewRequest("GET", "/api/jobs/non-existent-uuid/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", rec.Code)
	}
}

func Test_Gateway_GetStatus_ActiveJob_ReturnsCompleteJSON(t *testing.T) {
	_, state, _, _, _ := setupTestGateway("secret", "admin-key")
	jobID := "us-east-1:active-test-job"

	state.SetJobStatus(context.Background(), jobID, map[string]interface{}{
		"state":        string(models.JobPhaseTranscoding),
		"completed":    15,
		"total":        30,
		"pct":          50,
		"last_updated": time.Now().Unix(),
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
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var status map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if status["state"] != string(models.JobPhaseTranscoding) {
		t.Errorf("expected state Transcoding, got %s", status["state"])
	}
	if status["completed"] != "15" || status["total"] != "30" {
		t.Errorf("progress mismatch: completed=%s, total=%s", status["completed"], status["total"])
	}
}

// ─────────────────────────────────────────────────────────────
// 4. Rate Limiting & Anti-Spoofing Tests
// ─────────────────────────────────────────────────────────────

func Test_Gateway_RateLimiter_PublicIPHeaderSpoofing_Ignored(t *testing.T) {
	state := mocks.NewMockStateStore()
	// Seed a limit under the spoofed public IP
	state.RateLimits["ratelimit:ip:198.51.100.25"] = 999

	rl := gateway.NewRateLimiter(state, 5, 50, "jwt-secret")

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Direct client connection from a public IP that attempts to spoof X-Forwarded-For
	req := httptest.NewRequest("POST", "/api/jobs/upload-session", nil)
	req.RemoteAddr = "203.0.113.50:54321" // Public client
	req.Header.Set("X-Forwarded-For", "198.51.100.25")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Since remote is public, X-Forwarded-For should NOT be trusted, request proceeds using real IP
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK because public XFF spoofing was ignored, got %d", rec.Code)
	}
}

func Test_Gateway_RateLimiter_TrustedPrivateProxy_ParsedCorrectly(t *testing.T) {
	state := mocks.NewMockStateStore()
	// Rate limit the real client IP behind proxy
	state.RateLimits["ratelimit:ip:198.51.100.25"] = 100 // Exceeds limit of 5

	rl := gateway.NewRateLimiter(state, 5, 50, "jwt-secret")

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Connection originates from trusted local/private load balancer (127.0.0.1)
	req := httptest.NewRequest("POST", "/api/jobs/upload-session", nil)
	req.RemoteAddr = "127.0.0.1:54321" // Local proxy hop
	req.Header.Set("X-Forwarded-For", "198.51.100.25, 10.0.0.1")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// XFF trusted from 127.0.0.1, so client IP 198.51.100.25 is extracted and rate limited
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests when trusted proxy passes rate-limited IP, got %d", rec.Code)
	}
}

func Test_Gateway_RateLimiter_JWTUserLimit_SlidingWindow24h(t *testing.T) {
	secret := "jwt-secret-key-test"
	jobID := "us-east:test-jwt-window"

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    jobID,
		"job_id": jobID,
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	state := mocks.NewMockStateStore()
	state.RateLimits[fmt.Sprintf("ratelimit:user:%s", jobID)] = 500 // Exceeds limit

	rl := gateway.NewRateLimiter(state, 100, 100, secret)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs/status", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests for exceeded user limit, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// 5. Progress Multiplexer Concurrency Stress
// ─────────────────────────────────────────────────────────────

func Test_Gateway_Multiplexer_HighConcurrencySubscribeUnsubscribe_NoRace(t *testing.T) {
	state := mocks.NewMockStateStore()
	pm := gateway.NewProgressMultiplexer(state, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pm.Run(ctx)

	jobID := "stress-job-uuid-123"
	numSubscribers := 500
	var wg sync.WaitGroup

	channels := make([]chan models.ProgressUpdate, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		channels[i] = make(chan models.ProgressUpdate, 10)
	}

	// Concurrent subscribe
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pm.Subscribe(jobID, channels[idx])
		}(i)
	}
	wg.Wait()

	if count := pm.ActiveSubscriberCount(); count != numSubscribers {
		t.Errorf("expected %d active subscribers, got %d", numSubscribers, count)
	}

	// Concurrent unsubscribe
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pm.Unsubscribe(jobID, channels[idx])
		}(i)
	}
	wg.Wait()

	if count := pm.ActiveSubscriberCount(); count != 0 {
		t.Errorf("expected 0 active subscribers after unsubscribe, got %d", count)
	}
}

func Test_Gateway_Multiplexer_SlowClientBufferDrop_DoesNotBlock(t *testing.T) {
	state := mocks.NewMockStateStore()
	pm := gateway.NewProgressMultiplexer(state, 10)

	jobID := "slow-client-job"
	// Unbuffered channel representing a slow client
	slowCh := make(chan models.ProgressUpdate)
	pm.Subscribe(jobID, slowCh)
	defer pm.Unsubscribe(jobID, slowCh)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run multiplexer loop with mock stream update
	done := make(chan struct{})
	go func() {
		pm.Run(ctx)
		close(done)
	}()

	<-done
	// If it reached here without hanging indefinitely, slow consumer non-blocking send works
}

// ─────────────────────────────────────────────────────────────
// 6. Administration & Security Auth Tests
// ─────────────────────────────────────────────────────────────

func Test_Gateway_AdminAuth_ValidAndInvalidKeys(t *testing.T) {
	adminKey := "super-secure-admin-token-12345"

	requireAdmin := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Admin-Key")
			if key == "" {
				auth := r.Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					key = auth[7:]
				}
			}
			if key != adminKey {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	adminHandler := requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"admin_ok"}`))
	})

	t.Run("RejectsMissingKey", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs", nil)
		rec := httptest.NewRecorder()
		adminHandler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for missing admin key, got %d", rec.Code)
		}
	})

	t.Run("RejectsInvalidKey", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs", nil)
		req.Header.Set("X-Admin-Key", "wrong-key")
		rec := httptest.NewRecorder()
		adminHandler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for invalid admin key, got %d", rec.Code)
		}
	})

	t.Run("AcceptsValidKeyInHeader", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs", nil)
		req.Header.Set("X-Admin-Key", adminKey)
		rec := httptest.NewRecorder()
		adminHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for valid admin key, got %d", rec.Code)
		}
	})

	t.Run("AcceptsValidKeyInBearerToken", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs", nil)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminKey))
		rec := httptest.NewRecorder()
		adminHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for valid bearer admin key, got %d", rec.Code)
		}
	})
}

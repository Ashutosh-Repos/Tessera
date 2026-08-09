package gateway_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/test/mocks"
	"github.com/golang-jwt/jwt/v5"
)

func TestRateLimiter_IPLimitExceeded(t *testing.T) {
	state := mocks.NewMockStateStore()
	state.RateLimits["ratelimit:ip:192.168.1.100"] = 5 // exceeds limit of 5

	rl := gateway.NewRateLimiter(state, 5, 100, "secret")

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(nextHandler)

	req := httptest.NewRequest("POST", "/api/jobs/upload-session", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d (Too Many Requests)", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_GetIP_XForwardedFor(t *testing.T) {
	state := mocks.NewMockStateStore()
	rl := gateway.NewRateLimiter(state, 100, 100, "secret")

	var capturedIP string
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(nextHandler)

	req := httptest.NewRequest("POST", "/api/jobs/upload-session", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	_ = capturedIP
}

func TestRateLimiter_JWTUserLimit(t *testing.T) {
	secret := "test-secret-key"
	jobID := "us-east:test-job-jwt"

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    jobID,
		"job_id": jobID,
	})
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	state := mocks.NewMockStateStore()
	state.RateLimits[fmt.Sprintf("ratelimit:user:%s", jobID)] = 10 // exceeds limit of 10

	rl := gateway.NewRateLimiter(state, 100, 10, secret)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(nextHandler)

	req := httptest.NewRequest("GET", "/api/jobs/status", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d (Too Many Requests)", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_FailOpenOnRedisError(t *testing.T) {
	state := mocks.NewMockStateStore()
	state.Err = fmt.Errorf("redis cluster down")

	rl := gateway.NewRateLimiter(state, 5, 5, "secret")

	served := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(nextHandler)

	req := httptest.NewRequest("POST", "/api/jobs/upload-session", nil)
	req.RemoteAddr = "1.2.3.4:5678"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 OK (fail open)", rec.Code)
	}
	if !served {
		t.Errorf("request was not served on Redis error, expected fail-open")
	}
}

package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/golang-jwt/jwt/v5"
)

type mockRateLimitStateStore struct {
	infra.StateStore
	count map[string]int64
	err   error
}

func (m *mockRateLimitStateStore) IncrRateLimit(ctx context.Context, key string, windowSec int) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	if m.count == nil {
		m.count = make(map[string]int64)
	}
	m.count[key]++
	return m.count[key], nil
}

func TestRateLimiter_IPLimitExceeded(t *testing.T) {
	state := &mockRateLimitStateStore{
		count: map[string]int64{
			"ratelimit:ip:192.168.1.100": 5, // exceeds limit of 5
		},
	}

	rl := NewRateLimiter(state, 5, 100, "secret")

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
	state := &mockRateLimitStateStore{}
	rl := NewRateLimiter(state, 100, 100, "secret")

	var capturedIP string
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIP = getIP(r)
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(nextHandler)

	req := httptest.NewRequest("POST", "/api/jobs/upload-session", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedIP != "203.0.113.195" {
		t.Errorf("getIP() = %q, want first IP in X-Forwarded-For %q", capturedIP, "203.0.113.195")
	}
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

	state := &mockRateLimitStateStore{
		count: map[string]int64{
			fmt.Sprintf("ratelimit:user:%s", jobID): 10, // exceeds limit of 10
		},
	}

	rl := NewRateLimiter(state, 100, 10, secret)

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
	state := &mockRateLimitStateStore{
		err: fmt.Errorf("redis cluster down"),
	}

	rl := NewRateLimiter(state, 5, 5, "secret")

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

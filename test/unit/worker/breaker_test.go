package worker_test

import (
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/worker"
)

func TestCircuitBreaker(t *testing.T) {
	cb := worker.NewCircuitBreaker(1, 3)

	if cb.IsOpen() {
		t.Error("expected circuit breaker to start CLOSED")
	}

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.IsOpen() {
		t.Error("expected closed after 2 failures")
	}

	cb.RecordFailure()
	if !cb.IsOpen() {
		t.Error("expected OPEN after 3 failures")
	}

	d1 := cb.BackoffDuration()
	if d1 != 400*time.Millisecond {
		t.Errorf("expected backoff duration of 400ms, got %v", d1)
	}

	cb.RecordFailure()
	d2 := cb.BackoffDuration()
	if d2 != 800*time.Millisecond {
		t.Errorf("expected backoff duration of 800ms, got %v", d2)
	}

	cb.RecordSuccess()
	if cb.IsOpen() {
		t.Error("expected CLOSED after success")
	}
	if cb.BackoffDuration() != 100*time.Millisecond {
		t.Error("expected backoff reset to base duration")
	}
}

func TestCircuitBreaker_SlidingWindowExpiration(t *testing.T) {
	// Window of 100ms, threshold of 2
	cb := worker.NewCircuitBreaker(1, 2)

	cb.RecordFailure()
	time.Sleep(1100 * time.Millisecond) // Wait for window to expire

	// This failure alone should not open the breaker because the previous one expired
	cb.RecordFailure()
	if cb.IsOpen() {
		t.Error("expected CLOSED because first failure should have expired outside window")
	}

	// Immediate second failure within window opens breaker
	cb.RecordFailure()
	if !cb.IsOpen() {
		t.Error("expected OPEN after 2 failures within window")
	}
}

func TestCircuitBreaker_MaxBackoffCap(t *testing.T) {
	cb := worker.NewCircuitBreaker(5, 2)

	// Trigger 15 consecutive failures
	for i := 0; i < 15; i++ {
		cb.RecordFailure()
	}

	backoff := cb.BackoffDuration()
	if backoff > 5*time.Second {
		t.Errorf("expected backoff capped at 5s, got %v", backoff)
	}
}

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

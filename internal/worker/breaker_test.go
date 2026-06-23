package worker

import (
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(1, 3) // 3 failures within 1 second

	// Test 1: Starts closed
	if cb.IsOpen() {
		t.Error("expected circuit breaker to start CLOSED")
	}

	// Test 2: Trigger opening
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.IsOpen() {
		t.Error("expected closed after 2 failures")
	}

	cb.RecordFailure()
	if !cb.IsOpen() {
		t.Error("expected OPEN after 3 failures")
	}

	// Test 3: Backoff step calculation
	// For 3 consecutive failures: backoff is base * 2^(3-1) = 100ms * 4 = 400ms
	d1 := cb.BackoffDuration()
	if d1 != 400*time.Millisecond {
		t.Errorf("expected backoff duration of 400ms, got %v", d1)
	}

	cb.RecordFailure() // consecutive fails = 4: backoff is base * 2^(4-1) = 100ms * 8 = 800ms
	d2 := cb.BackoffDuration()
	if d2 != 800*time.Millisecond {
		t.Errorf("expected backoff duration of 800ms, got %v", d2)
	}

	// Test 4: Record success resets the state
	cb.RecordSuccess()
	if cb.IsOpen() {
		t.Error("expected CLOSED after success")
	}
	if cb.BackoffDuration() != 100*time.Millisecond {
		t.Error("expected backoff reset to base duration")
	}
}

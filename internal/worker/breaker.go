package worker

import (
	"sync"
	"time"
)

// CircuitBreaker prevents a thundering herd of S3 HEAD requests
// when Redis is temporarily unreachable.
type CircuitBreaker struct {
	mu               sync.Mutex
	failures         []time.Time // timestamps of recent failures
	windowDuration   time.Duration
	threshold        int
	open             bool
	backoffBase      time.Duration
	backoffMax       time.Duration
	consecutiveFails int
}

func NewCircuitBreaker(windowSec, threshold int) *CircuitBreaker {
	return &CircuitBreaker{
		windowDuration: time.Duration(windowSec) * time.Second,
		threshold:      threshold,
		backoffBase:    100 * time.Millisecond,
		backoffMax:     5 * time.Second,
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	now := time.Now()
	cb.failures = append(cb.failures, now)
	cb.consecutiveFails++

	// Trim old failures outside the window
	cutoff := now.Add(-cb.windowDuration)
	trimmed := cb.failures[:0]
	for _, t := range cb.failures {
		if t.After(cutoff) {
			trimmed = append(trimmed, t)
		}
	}
	cb.failures = trimmed

	if len(cb.failures) >= cb.threshold {
		cb.open = true
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFails = 0
	cb.open = false
}

// IsOpen returns true if Redis should not be contacted.
// When open, the caller must apply exponential backoff before S3 fallback.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.open
}

// BackoffDuration returns the current backoff duration based on consecutive failures.
func (cb *CircuitBreaker) BackoffDuration() time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	d := cb.backoffBase
	for i := 0; i < cb.consecutiveFails-1 && d < cb.backoffMax; i++ {
		d *= 2
	}
	if d > cb.backoffMax {
		d = cb.backoffMax
	}
	return d
}

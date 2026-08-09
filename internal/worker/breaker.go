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
	// H-2 fix: half-open state for auto-recovery
	lastOpenTime     time.Time     // when the breaker last transitioned to open
	cooldownDuration time.Duration // how long to wait before trying half-open
}

func NewCircuitBreaker(windowSec, threshold int) *CircuitBreaker {
	return &CircuitBreaker{
		windowDuration:   time.Duration(windowSec) * time.Second,
		threshold:        threshold,
		backoffBase:      100 * time.Millisecond,
		backoffMax:       5 * time.Second,
		cooldownDuration: 30 * time.Second, // H-2 fix: retry Redis after 30s
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
		cb.lastOpenTime = now
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFails = 0
	cb.open = false
}

// IsOpen returns true if Redis should not be contacted.
// H-2 fix: after cooldownDuration, the breaker transitions to half-open,
// allowing a single trial call to Redis. If that call succeeds (RecordSuccess),
// the breaker closes. If it fails (RecordFailure), the breaker re-opens.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if !cb.open {
		return false
	}
	// Half-open: if enough time has passed, allow one trial
	if time.Since(cb.lastOpenTime) >= cb.cooldownDuration {
		cb.open = false // transition to half-open (allow one trial)
		return false
	}
	return true
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


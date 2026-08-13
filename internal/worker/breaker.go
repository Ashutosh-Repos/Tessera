package worker

import (
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

// CircuitBreaker prevents a thundering herd of S3 HEAD requests
// when Redis is temporarily unreachable.
type CircuitBreaker struct {
	mu               sync.Mutex
	failures         []time.Time // timestamps of recent failures
	windowDuration   time.Duration
	threshold        int
	state            State
	backoffBase      time.Duration
	backoffMax       time.Duration
	consecutiveFails int
	lastOpenTime     time.Time     // when the breaker last transitioned to open
	cooldownDuration time.Duration // how long to wait before trying half-open
}

func NewCircuitBreaker(windowSec, threshold int) *CircuitBreaker {
	return &CircuitBreaker{
		windowDuration:   time.Duration(windowSec) * time.Second,
		threshold:        threshold,
		state:            StateClosed,
		backoffBase:      100 * time.Millisecond,
		backoffMax:       5 * time.Second,
		cooldownDuration: 30 * time.Second,
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	now := time.Now()
	cb.consecutiveFails++

	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.lastOpenTime = now
		return
	}

	cb.failures = append(cb.failures, now)

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
		cb.state = StateOpen
		cb.lastOpenTime = now
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFails = 0
	cb.state = StateClosed
	cb.failures = cb.failures[:0]
}

// IsOpen returns true if Redis should not be contacted.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	switch cb.state {
	case StateClosed:
		return false
	case StateOpen:
		if time.Since(cb.lastOpenTime) >= cb.cooldownDuration {
			cb.state = StateHalfOpen
			return false // allow one trial
		}
		return true
	case StateHalfOpen:
		return true // already allowed one trial, others must wait or fail
	}
	return false
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


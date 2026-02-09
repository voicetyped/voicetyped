package webhook

import (
	"sync"
	"time"
)

// Circuit breaker states.
const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
)

// CircuitBreakerConfig holds the parameters for a circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold    int
	ResetTimeout        time.Duration
	HalfOpenMaxAttempts int
}

// CircuitBreaker implements a per-endpoint circuit breaker pattern.
type CircuitBreaker struct {
	mu              sync.Mutex
	state           string
	failures        int
	successes       int
	lastFailureTime time.Time
	config          CircuitBreakerConfig
}

// NewCircuitBreaker creates a circuit breaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.HalfOpenMaxAttempts <= 0 {
		cfg.HalfOpenMaxAttempts = 1
	}
	return &CircuitBreaker{
		state:  StateClosed,
		config: cfg,
	}
}

// AllowRequest returns true if a request should be attempted.
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailureTime) > cb.config.ResetTimeout {
			cb.state = StateHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess records a successful delivery.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.config.HalfOpenMaxAttempts {
			cb.state = StateClosed
		}
		return
	}
	cb.state = StateClosed
}

// RecordFailure records a failed delivery.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		return
	}

	if cb.failures >= cb.config.FailureThreshold {
		cb.state = StateOpen
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

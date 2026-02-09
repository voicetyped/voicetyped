package webhook

import (
	"testing"
	"time"
)

func TestCircuitBreakerClosed(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     time.Second,
	})

	if !cb.AllowRequest() {
		t.Error("closed breaker should allow requests")
	}
	if cb.State() != StateClosed {
		t.Errorf("state = %q, want %q", cb.State(), StateClosed)
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		ResetTimeout:     time.Hour,
	})

	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Error("should still be closed after 1 failure")
	}

	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Errorf("state = %q, want %q after threshold", cb.State(), StateOpen)
	}

	if cb.AllowRequest() {
		t.Error("open breaker should not allow requests")
	}
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxAttempts: 1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatal("expected open")
	}

	time.Sleep(20 * time.Millisecond)

	if !cb.AllowRequest() {
		t.Error("should allow request after reset timeout (half-open)")
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("state = %q, want %q", cb.State(), StateHalfOpen)
	}

	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Errorf("state = %q, want %q after success in half-open", cb.State(), StateClosed)
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		ResetTimeout:     10 * time.Millisecond,
	})

	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.AllowRequest() // transitions to half-open

	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Errorf("state = %q, want %q after half-open failure", cb.State(), StateOpen)
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     time.Hour,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // resets counter
	cb.RecordFailure()

	if cb.State() != StateClosed {
		t.Error("success should reset failure count")
	}
}

package llm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type circuitState int

const (
	stateClosed circuitState = iota
	stateOpen
	stateHalfOpen
)

type CircuitBreakerProvider struct {
	inner         Provider
	mu            sync.Mutex
	state         circuitState
	failures      int
	lastFailure   time.Time
	failThreshold int
	resetTimeout  time.Duration
	fallbackErr   error
}

func NewCircuitBreakerProvider(inner Provider, failThreshold int, resetTimeout time.Duration) *CircuitBreakerProvider {
	if failThreshold <= 0 {
		failThreshold = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 30 * time.Second
	}
	return &CircuitBreakerProvider{
		inner:         inner,
		state:         stateClosed,
		failThreshold: failThreshold,
		resetTimeout:  resetTimeout,
		fallbackErr:   fmt.Errorf("circuit breaker open for provider %s", inner.Name()),
	}
}

func (cb *CircuitBreakerProvider) Name() string {
	return cb.inner.Name()
}

func (cb *CircuitBreakerProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	cb.mu.Lock()
	switch cb.state {
	case stateOpen:
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = stateHalfOpen
			cb.mu.Unlock()
		} else {
			cb.mu.Unlock()
			return nil, cb.fallbackErr
		}
	case stateHalfOpen:
		cb.mu.Unlock()
	default:
		cb.mu.Unlock()
	}

	resp, err := cb.inner.Complete(ctx, req)
	if err != nil {
		cb.recordFailure()
		return nil, err
	}

	cb.recordSuccess()
	return resp, nil
}

func (cb *CircuitBreakerProvider) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailure = time.Now()
	if cb.failures >= cb.failThreshold {
		cb.state = stateOpen
	}
}

func (cb *CircuitBreakerProvider) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = stateClosed
}

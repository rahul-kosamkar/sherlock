package llm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

type LLMError struct {
	Provider   string
	StatusCode int
	Message    string
	Retryable  bool
}

func (e *LLMError) Error() string {
	return fmt.Sprintf("%s: status %d: %s", e.Provider, e.StatusCode, e.Message)
}

func NewLLMError(provider string, statusCode int, message string) *LLMError {
	retryable := statusCode == 429 || (statusCode >= 500 && statusCode < 600)
	return &LLMError{
		Provider:   provider,
		StatusCode: statusCode,
		Message:    message,
		Retryable:  retryable,
	}
}

type RetryProvider struct {
	inner      Provider
	maxRetries int
	baseDelay  time.Duration
}

func NewRetryProvider(inner Provider, maxRetries int, baseDelay time.Duration) *RetryProvider {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if baseDelay <= 0 {
		baseDelay = 1 * time.Second
	}
	return &RetryProvider{
		inner:      inner,
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
	}
}

func (r *RetryProvider) Name() string {
	return r.inner.Name()
}

func (r *RetryProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		resp, err := r.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if !isRetryable(err) {
			return nil, err
		}

		if attempt < r.maxRetries {
			delay := r.backoff(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("all %d retries exhausted: %w", r.maxRetries, lastErr)
}

func (r *RetryProvider) backoff(attempt int) time.Duration {
	delay := float64(r.baseDelay) * math.Pow(2, float64(attempt))
	jitter := delay * 0.2 * (rand.Float64() - 0.5)
	return time.Duration(delay + jitter)
}

func isRetryable(err error) bool {
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr.Retryable
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return false
}

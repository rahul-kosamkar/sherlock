package llm

import (
	"context"
	"fmt"
	"time"
)

type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  float32
	MaxTokens    int
}

type CompletionResponse struct {
	Content      string
	InputTokens  int
	OutputTokens int
	Model        string
	Latency      time.Duration
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

type ProviderConfig struct {
	Provider    string // "openai", "vertex", "anthropic", "ollama"
	Model       string
	APIKey      string
	Endpoint    string
	GCPProject  string
	GCPRegion   string
	Temperature float32
	MaxTokens   int
	Timeout     time.Duration
}

func NewProvider(cfg ProviderConfig) (Provider, error) {
	var inner Provider
	switch cfg.Provider {
	case "openai":
		inner = NewOpenAIProvider(cfg)
	case "vertex":
		inner = NewVertexProvider(cfg)
	case "anthropic":
		inner = NewAnthropicProvider(cfg)
	case "ollama":
		inner = NewOllamaProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q", cfg.Provider)
	}

	retried := NewRetryProvider(inner, 3, 1*time.Second)
	return NewCircuitBreakerProvider(retried, 5, 30*time.Second), nil
}

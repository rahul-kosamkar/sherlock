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
	switch cfg.Provider {
	case "openai":
		return NewOpenAIProvider(cfg), nil
	case "vertex":
		return NewVertexProvider(cfg), nil
	case "anthropic":
		return NewAnthropicProvider(cfg), nil
	case "ollama":
		return NewOllamaProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q", cfg.Provider)
	}
}

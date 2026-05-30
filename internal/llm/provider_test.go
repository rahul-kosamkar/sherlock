package llm

import (
	"context"
	"testing"
	"time"
)

// MockProvider implements Provider for use in other test files.
type MockProvider struct {
	NameFunc     func() string
	CompleteFunc func(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Calls        []CompletionRequest
}

func (m *MockProvider) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock"
}

func (m *MockProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	m.Calls = append(m.Calls, req)
	if m.CompleteFunc != nil {
		return m.CompleteFunc(ctx, req)
	}
	return &CompletionResponse{Content: "mock response"}, nil
}

func TestNewProvider_OpenAI(t *testing.T) {
	p, err := NewProvider(ProviderConfig{Provider: "openai", Model: "gpt-4", APIKey: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Errorf("expected *OpenAIProvider, got %T", p)
	}
}

func TestNewProvider_Vertex(t *testing.T) {
	p, err := NewProvider(ProviderConfig{Provider: "vertex", GCPProject: "proj", GCPRegion: "us-central1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "vertex" {
		t.Errorf("Name() = %q, want %q", p.Name(), "vertex")
	}
	if _, ok := p.(*VertexProvider); !ok {
		t.Errorf("expected *VertexProvider, got %T", p)
	}
}

func TestNewProvider_Anthropic(t *testing.T) {
	p, err := NewProvider(ProviderConfig{Provider: "anthropic", Model: "claude-3", APIKey: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Errorf("expected *AnthropicProvider, got %T", p)
	}
}

func TestNewProvider_Ollama(t *testing.T) {
	p, err := NewProvider(ProviderConfig{Provider: "ollama", Model: "llama3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "ollama" {
		t.Errorf("Name() = %q, want %q", p.Name(), "ollama")
	}
	if _, ok := p.(*OllamaProvider); !ok {
		t.Errorf("expected *OllamaProvider, got %T", p)
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := NewProvider(ProviderConfig{Provider: "gemini"})
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	want := `unsupported LLM provider: "gemini"`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestNewProvider_EmptyProvider(t *testing.T) {
	_, err := NewProvider(ProviderConfig{Provider: ""})
	if err == nil {
		t.Fatal("expected error for empty provider, got nil")
	}
	want := `unsupported LLM provider: ""`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestProviderConfig_Defaults(t *testing.T) {
	cfg := ProviderConfig{
		Provider:    "openai",
		Model:       "gpt-4-turbo",
		APIKey:      "test-key",
		Temperature: 0.7,
		MaxTokens:   4096,
		Timeout:     30 * time.Second,
	}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	oai, ok := p.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", p)
	}
	if oai.model != "gpt-4-turbo" {
		t.Errorf("model = %q, want %q", oai.model, "gpt-4-turbo")
	}
	if oai.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", oai.apiKey, "test-key")
	}
	if oai.temperature != 0.7 {
		t.Errorf("temperature = %v, want %v", oai.temperature, 0.7)
	}
	if oai.maxTokens != 4096 {
		t.Errorf("maxTokens = %d, want %d", oai.maxTokens, 4096)
	}
	if oai.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want %v", oai.timeout, 30*time.Second)
	}
	if oai.endpoint != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("endpoint = %q, want default OpenAI endpoint", oai.endpoint)
	}
}

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newAnthropicTestProvider(url string) *AnthropicProvider {
	return NewAnthropicProvider(ProviderConfig{
		Provider:    "anthropic",
		Model:       "claude-3-sonnet",
		APIKey:      "test-anthropic-key",
		Endpoint:    url,
		Temperature: 0.3,
		MaxTokens:   2048,
		Timeout:     5 * time.Second,
	})
}

func TestAnthropic_Complete_Success(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": "The root cause is a memory leak"},
			},
			"usage": map[string]int{
				"input_tokens":  200,
				"output_tokens": 80,
			},
			"model": "claude-3-sonnet-20240229",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newAnthropicTestProvider(ts.URL)
	got, err := p.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "You are an SRE.",
		UserPrompt:   "Analyze this alert.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "The root cause is a memory leak" {
		t.Errorf("Content = %q, want %q", got.Content, "The root cause is a memory leak")
	}
	if got.InputTokens != 200 {
		t.Errorf("InputTokens = %d, want %d", got.InputTokens, 200)
	}
	if got.OutputTokens != 80 {
		t.Errorf("OutputTokens = %d, want %d", got.OutputTokens, 80)
	}
	if got.Model != "claude-3-sonnet-20240229" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-3-sonnet-20240229")
	}
}

func TestAnthropic_Complete_WithSystemPrompt(t *testing.T) {
	t.Parallel()
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]int{},
			"model": "claude-3-sonnet",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newAnthropicTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "You are an expert.",
		UserPrompt:   "Analyze.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	systemField, ok := receivedBody["system"]
	if !ok {
		t.Fatal("expected 'system' field in request body when SystemPrompt is set")
	}
	if systemField != "You are an expert." {
		t.Errorf("system = %q, want %q", systemField, "You are an expert.")
	}
}

func TestAnthropic_Complete_NoSystemPrompt(t *testing.T) {
	t.Parallel()
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]int{},
			"model": "claude-3-sonnet",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newAnthropicTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "",
		UserPrompt:   "Analyze.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := receivedBody["system"]; ok {
		t.Error("expected 'system' field to be absent when SystemPrompt is empty")
	}
}

func TestAnthropic_Complete_RateLimit(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer ts.Close()

	p := newAnthropicTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatalf("expected *LLMError, got %T: %v", err, err)
	}
	if llmErr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", llmErr.StatusCode)
	}
	if !llmErr.Retryable {
		t.Error("expected Retryable to be true for 429")
	}
}

func TestAnthropic_Complete_ServerError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`server overloaded`))
	}))
	defer ts.Close()

	p := newAnthropicTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain status code 500", err.Error())
	}
	if !strings.Contains(err.Error(), "server overloaded") {
		t.Errorf("error = %q, want it to contain response body", err.Error())
	}
}

func TestAnthropic_Complete_EmptyContent(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content":[],"usage":{},"model":"claude-3-sonnet"}`))
	}))
	defer ts.Close()

	p := newAnthropicTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no content") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no content")
	}
}

func TestAnthropic_Complete_Headers(t *testing.T) {
	t.Parallel()
	var gotAPIKey, gotVersion string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")

		resp := map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]int{},
			"model": "claude-3-sonnet",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newAnthropicTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAPIKey != "test-anthropic-key" {
		t.Errorf("x-api-key = %q, want %q", gotAPIKey, "test-anthropic-key")
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, "2023-06-01")
	}
}

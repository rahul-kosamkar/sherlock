package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newOpenAITestProvider(url string) *OpenAIProvider {
	return NewOpenAIProvider(ProviderConfig{
		Provider:    "openai",
		Model:       "gpt-4",
		APIKey:      "test-key",
		Endpoint:    url,
		Temperature: 0.3,
		MaxTokens:   2048,
		Timeout:     5 * time.Second,
	})
}

func TestOpenAI_Complete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "Root cause is OOM"}},
			},
			"usage": map[string]int{
				"prompt_tokens":     100,
				"completion_tokens": 50,
			},
			"model": "gpt-4-turbo",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newOpenAITestProvider(ts.URL)
	got, err := p.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "You are an SRE.",
		UserPrompt:   "Analyze this alert.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "Root cause is OOM" {
		t.Errorf("Content = %q, want %q", got.Content, "Root cause is OOM")
	}
	if got.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want %d", got.InputTokens, 100)
	}
	if got.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want %d", got.OutputTokens, 50)
	}
	if got.Model != "gpt-4-turbo" {
		t.Errorf("Model = %q, want %q", got.Model, "gpt-4-turbo")
	}
	if got.Latency <= 0 {
		t.Errorf("Latency = %v, want > 0", got.Latency)
	}
}

func TestOpenAI_Complete_RateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer ts.Close()

	p := newOpenAITestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "rate limited")
	}
}

func TestOpenAI_Complete_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal server error`))
	}))
	defer ts.Close()

	p := newOpenAITestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain status code 500", err.Error())
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Errorf("error = %q, want it to contain response body", err.Error())
	}
}

func TestOpenAI_Complete_EmptyChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[],"usage":{},"model":"gpt-4"}`))
	}))
	defer ts.Close()

	p := newOpenAITestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no choices")
	}
}

func TestOpenAI_Complete_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not valid json`))
	}))
	defer ts.Close()

	p := newOpenAITestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "unmarshal")
	}
}

func TestOpenAI_Complete_SystemPromptOmitted(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}},
			},
			"usage": map[string]int{},
			"model": "gpt-4",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newOpenAITestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "",
		UserPrompt:   "just a user message",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, ok := receivedBody["messages"].([]any)
	if !ok {
		t.Fatal("messages field missing or not an array")
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message (user only), got %d", len(messages))
	}
	msg := messages[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("role = %q, want %q", msg["role"], "user")
	}
}

func TestOpenAI_Complete_CustomEndpoint(t *testing.T) {
	var requestReceived bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "response"}},
			},
			"usage": map[string]int{},
			"model": "custom-model",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := NewOpenAIProvider(ProviderConfig{
		Provider: "openai",
		Model:    "custom-model",
		APIKey:   "key",
		Endpoint: ts.URL,
		Timeout:  5 * time.Second,
	})

	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requestReceived {
		t.Error("request was not sent to the custom endpoint")
	}
}

func TestOpenAI_Complete_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	p := NewOpenAIProvider(ProviderConfig{
		Provider: "openai",
		Model:    "gpt-4",
		APIKey:   "key",
		Endpoint: ts.URL,
		Timeout:  100 * time.Millisecond,
	})

	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "request failed") {
		t.Errorf("error = %q, want it to indicate a timeout", err.Error())
	}
}

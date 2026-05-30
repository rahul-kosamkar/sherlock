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

func newOllamaTestProvider(url string) *OllamaProvider {
	return NewOllamaProvider(ProviderConfig{
		Provider:    "ollama",
		Model:       "llama3",
		Endpoint:    url,
		Temperature: 0.5,
		MaxTokens:   1024,
		Timeout:     5 * time.Second,
	})
}

func TestOllama_Complete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/api/chat")
		}
		resp := map[string]any{
			"message": map[string]string{"content": "Analysis complete"},
			"model":   "llama3:latest",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newOllamaTestProvider(ts.URL)
	got, err := p.Complete(context.Background(), CompletionRequest{
		UserPrompt: "Analyze this.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "Analysis complete" {
		t.Errorf("Content = %q, want %q", got.Content, "Analysis complete")
	}
	if got.Model != "llama3:latest" {
		t.Errorf("Model = %q, want %q", got.Model, "llama3:latest")
	}
	if got.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0", got.InputTokens)
	}
	if got.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0", got.OutputTokens)
	}
}

func TestOllama_Complete_WithSystemPrompt(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"message": map[string]string{"content": "ok"},
			"model":   "llama3",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newOllamaTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "You are an SRE.",
		UserPrompt:   "Analyze.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, ok := receivedBody["messages"].([]any)
	if !ok {
		t.Fatal("messages field missing or not an array")
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(messages))
	}
	sysMsg := messages[0].(map[string]any)
	if sysMsg["role"] != "system" {
		t.Errorf("first message role = %q, want %q", sysMsg["role"], "system")
	}
	if sysMsg["content"] != "You are an SRE." {
		t.Errorf("system content = %q, want %q", sysMsg["content"], "You are an SRE.")
	}
}

func TestOllama_Complete_StreamDisabled(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"message": map[string]string{"content": "ok"},
			"model":   "llama3",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newOllamaTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	streamVal, ok := receivedBody["stream"]
	if !ok {
		t.Fatal("expected 'stream' field in request body")
	}
	if streamVal != false {
		t.Errorf("stream = %v, want false", streamVal)
	}
}

func TestOllama_Complete_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`model not found`))
	}))
	defer ts.Close()

	p := newOllamaTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{UserPrompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain status code 500", err.Error())
	}
}

func TestOllama_Complete_DefaultEndpoint(t *testing.T) {
	p := NewOllamaProvider(ProviderConfig{
		Provider: "ollama",
		Model:    "llama3",
	})
	if p.endpoint != "http://localhost:11434" {
		t.Errorf("endpoint = %q, want %q", p.endpoint, "http://localhost:11434")
	}
}

func TestOllama_Complete_CustomEndpoint(t *testing.T) {
	var requestReceived bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		resp := map[string]any{
			"message": map[string]string{"content": "ok"},
			"model":   "llama3",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := NewOllamaProvider(ProviderConfig{
		Provider: "ollama",
		Model:    "llama3",
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

func TestOllama_Complete_Options(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"message": map[string]string{"content": "ok"},
			"model":   "llama3",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	p := newOllamaTestProvider(ts.URL)
	_, err := p.Complete(context.Background(), CompletionRequest{
		UserPrompt:  "test",
		Temperature: 0.8,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	opts, ok := receivedBody["options"].(map[string]any)
	if !ok {
		t.Fatal("expected 'options' field in request body")
	}

	temp, ok := opts["temperature"].(float64)
	if !ok {
		t.Fatal("expected 'temperature' in options")
	}
	if temp != 0.8 {
		t.Errorf("temperature = %v, want %v", temp, 0.8)
	}

	numPredict, ok := opts["num_predict"].(float64)
	if !ok {
		t.Fatal("expected 'num_predict' in options")
	}
	if int(numPredict) != 512 {
		t.Errorf("num_predict = %v, want %v", numPredict, 512)
	}
}

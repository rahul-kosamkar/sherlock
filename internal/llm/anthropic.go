package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AnthropicProvider struct {
	apiKey      string
	model       string
	endpoint    string
	temperature float32
	maxTokens   int
	timeout     time.Duration
	client      *http.Client
}

func NewAnthropicProvider(cfg ProviderConfig) *AnthropicProvider {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	return &AnthropicProvider{
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		endpoint:    endpoint,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		timeout:     timeout,
		client:      &http.Client{Timeout: timeout},
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	start := time.Now()

	temp := req.Temperature
	if temp == 0 {
		temp = p.temperature
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.maxTokens
	}

	body := map[string]any{
		"model":       p.model,
		"max_tokens":  maxTokens,
		"temperature": temp,
		"messages": []map[string]string{
			{"role": "user", "content": req.UserPrompt},
		},
	}
	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewLLMError("anthropic", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal response: %w", err)
	}

	if len(result.Content) == 0 {
		return nil, fmt.Errorf("anthropic: no content in response")
	}

	return &CompletionResponse{
		Content:      result.Content[0].Text,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		Model:        result.Model,
		Latency:      time.Since(start),
	}, nil
}

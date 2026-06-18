package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

type VertexProvider struct {
	project     string
	region      string
	model       string
	temperature float32
	maxTokens   int
	timeout     time.Duration
	client      *genai.Client
	clientOnce  sync.Once
	clientErr   error
}

func NewVertexProvider(cfg ProviderConfig) *VertexProvider {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &VertexProvider{
		project:     cfg.GCPProject,
		region:      cfg.GCPRegion,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		timeout:     timeout,
	}
}

func (p *VertexProvider) Name() string {
	return "vertex"
}

func (p *VertexProvider) getClient(ctx context.Context) (*genai.Client, error) {
	p.clientOnce.Do(func() {
		p.client, p.clientErr = genai.NewClient(ctx, &genai.ClientConfig{
			Project:  p.project,
			Location: p.region,
			Backend:  genai.BackendVertexAI,
		})
	})
	return p.client, p.clientErr
}

func (p *VertexProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	client, err := p.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("vertex: create client: %w", err)
	}

	var userContent string
	if req.SystemPrompt != "" {
		userContent = req.SystemPrompt + "\n\n" + req.UserPrompt
	} else {
		userContent = req.UserPrompt
	}

	temp := req.Temperature
	if temp == 0 {
		temp = p.temperature
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.maxTokens
	}

	config := &genai.GenerateContentConfig{
		Temperature:     &temp,
		MaxOutputTokens: int32(maxTokens),
	}

	result, err := client.Models.GenerateContent(ctx, p.model, genai.Text(userContent), config)
	if err != nil {
		return nil, fmt.Errorf("vertex: generate content: %w", err)
	}

	var content strings.Builder
	if result.Candidates != nil {
		for _, candidate := range result.Candidates {
			if candidate.Content != nil {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						content.WriteString(part.Text)
					}
				}
			}
		}
	}

	var inputTokens, outputTokens int
	if result.UsageMetadata != nil {
		inputTokens = int(result.UsageMetadata.PromptTokenCount)
		outputTokens = int(result.UsageMetadata.CandidatesTokenCount)
	}

	return &CompletionResponse{
		Content:      content.String(),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Model:        p.model,
		Latency:      time.Since(start),
	}, nil
}

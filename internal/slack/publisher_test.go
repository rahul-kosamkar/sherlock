package slack

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	slackapi "github.com/slack-go/slack"
	"go.uber.org/zap"
)

type mockSlackAPI struct {
	channel string
	ts      string
	err     error
	calls   int
}

func (m *mockSlackAPI) PostMessageContext(_ context.Context, channelID string, _ ...slackapi.MsgOption) (string, string, error) {
	m.channel = channelID
	m.calls++
	return channelID, m.ts, m.err
}

func TestConfidenceIndicator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		confidence float64
		want       string
	}{
		{"high_0.9", 0.9, "High"},
		{"high_0.71", 0.71, "High"},
		{"high_boundary", 0.7, "Medium"},
		{"medium_0.5", 0.5, "Medium"},
		{"medium_0.4", 0.4, "Medium"},
		{"low_0.39", 0.39, "Low"},
		{"low_0.1", 0.1, "Low"},
		{"low_zero", 0.0, "Low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := confidenceIndicator(tt.confidence)
			if got != tt.want {
				t.Errorf("confidenceIndicator(%f) = %q, want %q", tt.confidence, got, tt.want)
			}
		})
	}
}

func TestConfidenceColor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		confidence float64
		want       string
	}{
		{"high", 0.85, ":large_green_circle:"},
		{"medium", 0.5, ":large_yellow_circle:"},
		{"low", 0.2, ":red_circle:"},
		{"boundary_high", 0.71, ":large_green_circle:"},
		{"boundary_medium", 0.4, ":large_yellow_circle:"},
		{"boundary_low", 0.39, ":red_circle:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := confidenceColor(tt.confidence)
			if got != tt.want {
				t.Errorf("confidenceColor(%f) = %q, want %q", tt.confidence, got, tt.want)
			}
		})
	}
}

func TestTruncateSlackText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"long", "hello world this is long", 10, "hello w..."},
		{"empty", "", 10, ""},
		{"one_char_max", "abc", 4, "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateSlackText(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateSlackText(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestBuildResultBlocks_RoutesToRuleBasedForNonLLM(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Test Headline",
		Confidence:      0.8,
		RCAEngine:       "rule-based",
	}

	blocks := p.buildResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks, got none")
	}
}

func TestBuildResultBlocks_RoutesToLLMForLLMEngine(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Test Headline",
		Confidence:      0.85,
		RCAEngine:       "llm-powered",
		AIProvider:      "openai",
		AIModel:         "gpt-4o",
		PassCount:       2,
		RootCause:       "Memory leak in handler",
		Severity:        "high",
		BugFixable:      true,
		TopHypotheses: []contracts.Hypothesis{
			{Title: "OOM Kill", Narrative: "Memory leak"},
		},
		RecommendedActions: []contracts.SuggestedFix{
			{Title: "Increase memory", Description: "Set to 512Mi"},
			{Title: "Fix leak", Description: "Use defer to close resources"},
		},
	}

	blocks := p.buildResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks, got none")
	}

	if len(blocks) < 5 {
		t.Errorf("expected at least 5 blocks for LLM result, got %d", len(blocks))
	}
}

func TestBuildRuleResultBlocks_Structure(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "CrashLoop Detected",
		Confidence:      0.75,
		TopHypotheses: []contracts.Hypothesis{
			{
				Title:     "CrashLoopBackOff",
				Narrative: "Pod is crash-looping due to OOM",
			},
		},
		RecommendedActions: []contracts.SuggestedFix{
			{Title: "Increase memory limit"},
		},
		TimelineEventIDs: []string{"ev1", "ev2"},
	}

	blocks := p.buildRuleResultBlocks(result)

	if len(blocks) < 4 {
		t.Errorf("expected at least 4 blocks, got %d", len(blocks))
	}
}

func TestBuildRuleResultBlocks_NoHypotheses(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "No clear hypothesis",
		Confidence:      0.0,
	}

	blocks := p.buildRuleResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks even with no hypotheses")
	}
}

func TestBuildRuleResultBlocks_NoActions(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Test",
		Confidence:      0.5,
		TopHypotheses: []contracts.Hypothesis{
			{Title: "Test", Narrative: "Test narrative"},
		},
	}

	blocks := p.buildRuleResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks")
	}
}

func TestBuildLLMResultBlocks_Structure(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "OOM in payment-service",
		Confidence:      0.85,
		RCAEngine:       "llm-powered",
		AIProvider:      "openai",
		AIModel:         "gpt-4o",
		PassCount:       3,
		RootCause:       "Memory leak in QuickbooksUseCases.syncInvoice caused unhandled growth",
		Severity:        "critical",
		BugFixable:      true,
		TopHypotheses: []contracts.Hypothesis{
			{
				Title:     "OOM due to memory leak",
				Narrative: "Detailed narrative",
			},
		},
		RecommendedActions: []contracts.SuggestedFix{
			{Title: "Increase memory", Description: "Set to 1Gi"},
			{Title: "Fix sync function", Description: "Add proper cleanup"},
		},
		TimelineEventIDs: []string{"ev1"},
	}

	blocks := p.buildLLMResultBlocks(result)

	if len(blocks) < 7 {
		t.Errorf("expected at least 7 blocks for full LLM result, got %d", len(blocks))
	}
}

func TestBuildLLMResultBlocks_SinglePass(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Error spike",
		Confidence:      0.65,
		RCAEngine:       "llm-powered",
		PassCount:       1,
	}

	blocks := p.buildLLMResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks")
	}
}

func TestBuildLLMResultBlocks_NoRootCause(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Test",
		Confidence:      0.5,
		RCAEngine:       "llm-powered",
		PassCount:       2,
	}

	blocks := p.buildLLMResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks even without root cause")
	}
}

func TestBuildLLMResultBlocks_NoSeverity(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Test",
		Confidence:      0.85,
		RCAEngine:       "llm-powered",
		PassCount:       2,
		RootCause:       "Some root cause",
	}

	blocks := p.buildLLMResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks")
	}
}

func TestBuildLLMResultBlocks_ManyRecommendations(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	actions := make([]contracts.SuggestedFix, 8)
	for i := range actions {
		actions[i] = contracts.SuggestedFix{Title: "Step", Description: "Do something"}
	}
	result := &contracts.InvestigationResult{
		InvestigationID:    "inv-1",
		Headline:           "Test",
		Confidence:         0.85,
		RCAEngine:          "llm-powered",
		PassCount:          2,
		RecommendedActions: actions,
	}

	blocks := p.buildLLMResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks")
	}
}

func TestBuildLLMResultBlocks_NoAIMetadata(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Test",
		Confidence:      0.5,
		RCAEngine:       "llm-powered",
	}

	blocks := p.buildLLMResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks")
	}
}

func TestBuildActionButtons(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-123",
	}

	blocks := p.buildActionButtons(result)
	if len(blocks) != 2 {
		t.Errorf("expected 2 action blocks, got %d", len(blocks))
	}
}

// --- Additional Tests ---

func TestConfidenceIndicator_AllRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		confidence float64
		want       string
	}{
		{"above_0.9", 0.95, "High"},
		{"at_0.75", 0.75, "High"},
		{"at_0.71", 0.71, "High"},
		{"at_boundary_0.7", 0.7, "Medium"},
		{"at_0.5", 0.5, "Medium"},
		{"at_0.4", 0.4, "Medium"},
		{"below_0.4", 0.39, "Low"},
		{"at_zero", 0.0, "Low"},
		{"at_1.0", 1.0, "High"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := confidenceIndicator(tt.confidence)
			if got != tt.want {
				t.Errorf("confidenceIndicator(%f) = %q, want %q", tt.confidence, got, tt.want)
			}
		})
	}
}

func TestConfidenceColor_AllRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		confidence float64
		want       string
	}{
		{"above_0.9", 0.95, ":large_green_circle:"},
		{"at_0.75", 0.75, ":large_green_circle:"},
		{"at_boundary_0.7", 0.7, ":large_yellow_circle:"},
		{"at_0.5", 0.5, ":large_yellow_circle:"},
		{"at_0.4", 0.4, ":large_yellow_circle:"},
		{"below_0.4", 0.39, ":red_circle:"},
		{"at_zero", 0.0, ":red_circle:"},
		{"at_1.0", 1.0, ":large_green_circle:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := confidenceColor(tt.confidence)
			if got != tt.want {
				t.Errorf("confidenceColor(%f) = %q, want %q", tt.confidence, got, tt.want)
			}
		})
	}
}

func TestTruncateSlackText_ExactLength(t *testing.T) {
	t.Parallel()
	input := "exact"
	got := truncateSlackText(input, 5)
	if got != input {
		t.Errorf("truncateSlackText(%q, 5) = %q, want %q", input, got, input)
	}
}

func TestBuildRuleResultBlocks_MultipleHypotheses(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-multi",
		Headline:        "Multiple issues found",
		Confidence:      0.8,
		TopHypotheses: []contracts.Hypothesis{
			{Title: "OOM Kill", Narrative: "Container ran out of memory"},
			{Title: "Disk Full", Narrative: "Root volume at 100%"},
			{Title: "Network Timeout", Narrative: "DNS resolution failures"},
		},
	}

	blocks := p.buildRuleResultBlocks(result)
	if len(blocks) == 0 {
		t.Fatal("expected blocks, got none")
	}

	found := false
	for _, b := range blocks {
		if sec, ok := b.(*slackapi.SectionBlock); ok {
			if sec.Text != nil && strings.Contains(sec.Text.Text, "OOM Kill") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected top hypothesis (OOM Kill) in blocks")
	}
}

func TestBuildLLMResultBlocks_WithRootCause(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-rc",
		Headline:        "Memory leak detected",
		Confidence:      0.9,
		RCAEngine:       "llm-powered",
		PassCount:       2,
		RootCause:       "Unbounded goroutine leak in event handler",
	}

	blocks := p.buildLLMResultBlocks(result)

	found := false
	for _, b := range blocks {
		if sec, ok := b.(*slackapi.SectionBlock); ok {
			if sec.Text != nil && strings.Contains(sec.Text.Text, "Root Cause") &&
				strings.Contains(sec.Text.Text, "Unbounded goroutine leak") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected root cause section in LLM result blocks")
	}
}

func TestBuildLLMResultBlocks_WithMetadata(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID: "inv-meta",
		Headline:        "Service degradation",
		Confidence:      0.75,
		RCAEngine:       "llm-powered",
		AIProvider:      "anthropic",
		AIModel:         "claude-3",
		PassCount:       3,
	}

	blocks := p.buildLLMResultBlocks(result)

	found := false
	for _, b := range blocks {
		if ctx, ok := b.(*slackapi.ContextBlock); ok {
			for _, el := range ctx.ContextElements.Elements {
				if txt, ok := el.(*slackapi.TextBlockObject); ok {
					if strings.Contains(txt.Text, "anthropic") &&
						strings.Contains(txt.Text, "claude-3") &&
						strings.Contains(txt.Text, "3") {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Error("expected metadata footer with AIProvider, AIModel, and PassCount")
	}
}

func TestBuildLLMResultBlocks_NoRecommendedActions(t *testing.T) {
	t.Parallel()
	p := &Publisher{}
	result := &contracts.InvestigationResult{
		InvestigationID:    "inv-nofix",
		Headline:           "Transient error",
		Confidence:         0.6,
		RCAEngine:          "llm-powered",
		PassCount:          2,
		RecommendedActions: nil,
	}

	blocks := p.buildLLMResultBlocks(result)

	for _, b := range blocks {
		if sec, ok := b.(*slackapi.SectionBlock); ok {
			if sec.Text != nil && strings.Contains(sec.Text.Text, "Recommendations:") {
				t.Error("did not expect Recommendations section when no actions provided")
			}
		}
	}
}

func TestBuildResultBlocks_RoutesBasedOnEngine(t *testing.T) {
	t.Parallel()
	p := &Publisher{}

	t.Run("llm-powered", func(t *testing.T) {
		t.Parallel()
		result := &contracts.InvestigationResult{
			InvestigationID: "inv-llm",
			Headline:        "LLM analysis",
			Confidence:      0.8,
			RCAEngine:       "llm-powered",
			PassCount:       2,
		}
		blocks := p.buildResultBlocks(result)
		if len(blocks) == 0 {
			t.Fatal("expected blocks for llm-powered")
		}
		header, ok := blocks[0].(*slackapi.HeaderBlock)
		if !ok {
			t.Fatal("expected first block to be HeaderBlock")
		}
		if !strings.Contains(header.Text.Text, "Deep Investigation") {
			t.Errorf("LLM header = %q, expected to contain 'Deep Investigation'", header.Text.Text)
		}
	})

	t.Run("rule-based", func(t *testing.T) {
		t.Parallel()
		result := &contracts.InvestigationResult{
			InvestigationID: "inv-rule",
			Headline:        "Rule analysis",
			Confidence:      0.7,
			RCAEngine:       "rule-based",
		}
		blocks := p.buildResultBlocks(result)
		if len(blocks) == 0 {
			t.Fatal("expected blocks for rule-based")
		}
		header, ok := blocks[0].(*slackapi.HeaderBlock)
		if !ok {
			t.Fatal("expected first block to be HeaderBlock")
		}
		if header.Text.Text != "Rule analysis" {
			t.Errorf("Rule header = %q, want %q", header.Text.Text, "Rule analysis")
		}
	})
}

func TestNewPublisher(t *testing.T) {
	t.Parallel()
	p := NewPublisher("xoxb-test-token", zap.NewNop())
	if p == nil {
		t.Fatal("NewPublisher returned nil")
	}
	if p.slackClient == nil {
		t.Fatal("slackClient should not be nil")
	}
}

func TestPostInvestigationStarted_Success(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{ts: "1234567890.123456"}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	ts, err := p.PostInvestigationStarted(context.Background(), "C-test", "inv-123")
	if err != nil {
		t.Fatalf("PostInvestigationStarted() error = %v", err)
	}
	if ts != "1234567890.123456" {
		t.Errorf("ts = %q, want %q", ts, "1234567890.123456")
	}
	if mock.channel != "C-test" {
		t.Errorf("channel = %q, want %q", mock.channel, "C-test")
	}
}

func TestPostInvestigationStarted_WithoutID(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{ts: "ts-1"}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	ts, err := p.PostInvestigationStarted(context.Background(), "C-chan", "")
	if err != nil {
		t.Fatalf("PostInvestigationStarted() error = %v", err)
	}
	if ts != "ts-1" {
		t.Errorf("ts = %q, want %q", ts, "ts-1")
	}
}

func TestPostInvestigationStarted_Error(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{err: errors.New("slack error")}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	_, err := p.PostInvestigationStarted(context.Background(), "C-test", "inv-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostEvidenceUpdate_Success(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{ts: "ts-1"}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	err := p.PostEvidenceUpdate(context.Background(), "C-test", "ts-thread", "Collecting evidence...")
	if err != nil {
		t.Fatalf("PostEvidenceUpdate() error = %v", err)
	}
	if mock.channel != "C-test" {
		t.Errorf("channel = %q, want %q", mock.channel, "C-test")
	}
}

func TestPostEvidenceUpdate_Error(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{err: errors.New("network error")}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	err := p.PostEvidenceUpdate(context.Background(), "C-test", "ts-1", "update")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostResult_Success(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{ts: "ts-1"}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "OOM kill detected",
		Confidence:      0.85,
		RCAEngine:       "rule-based",
	}
	err := p.PostResult(context.Background(), "C-test", "ts-thread", result)
	if err != nil {
		t.Fatalf("PostResult() error = %v", err)
	}
}

func TestPostResult_Error(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{err: errors.New("api error")}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	result := &contracts.InvestigationResult{
		InvestigationID: "inv-1",
		Headline:        "Test",
		RCAEngine:       "rule-based",
	}
	err := p.PostResult(context.Background(), "C-test", "ts-1", result)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNotifyPassStarted_Success(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{ts: "ts-1"}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	err := p.NotifyPassStarted(context.Background(), "C-test", "ts-thread", 2, "Starting pass 2...")
	if err != nil {
		t.Fatalf("NotifyPassStarted() error = %v", err)
	}
}

func TestNotifyPassStarted_Error(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{err: errors.New("timeout")}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	err := p.NotifyPassStarted(context.Background(), "C-test", "ts-1", 1, "pass 1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostError_Success(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{ts: "ts-1"}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	err := p.PostError(context.Background(), "C-test", "ts-thread", "something broke")
	if err != nil {
		t.Fatalf("PostError() error = %v", err)
	}
}

func TestPostError_WithoutThread(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{ts: "ts-1"}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	err := p.PostError(context.Background(), "C-test", "", "error without thread")
	if err != nil {
		t.Fatalf("PostError() error = %v", err)
	}
}

func TestPostError_Error(t *testing.T) {
	t.Parallel()
	mock := &mockSlackAPI{err: errors.New("api down")}
	p := &Publisher{slackClient: mock, logger: zap.NewNop()}

	err := p.PostError(context.Background(), "C-test", "ts-1", "test error")
	if err == nil {
		t.Fatal("expected error")
	}
}

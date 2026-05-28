package rca

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/llm"
	"go.uber.org/zap"
)

// --- Mocks ---

type mockLLMProvider struct {
	nameVal   string
	responses []*llm.CompletionResponse
	errors    []error
	callIdx   int
	calls     []llm.CompletionRequest
}

func (m *mockLLMProvider) Name() string { return m.nameVal }

func (m *mockLLMProvider) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	idx := m.callIdx
	m.callIdx++
	m.calls = append(m.calls, req)
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return nil, fmt.Errorf("no more responses")
}

type mockFallbackEngine struct {
	hypotheses []contracts.Hypothesis
	err        error
	called     bool
}

func (m *mockFallbackEngine) Name() string { return "mock-fallback" }

func (m *mockFallbackEngine) Rank(ctx context.Context, graph contracts.InvestigationGraph) ([]contracts.Hypothesis, error) {
	m.called = true
	return m.hypotheses, m.err
}

type mockPassNotifier struct {
	notifications []string
}

func (m *mockPassNotifier) NotifyPassStarted(ctx context.Context, channelID, threadTS string, passNum int, message string) error {
	m.notifications = append(m.notifications, message)
	return nil
}

type mockFollowUpCollectorSet struct {
	evidence []contracts.Evidence
	err      error
}

func (m *mockFollowUpCollectorSet) CollectAll(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	return m.evidence, m.err
}

// --- Test Data ---

func testGraph() contracts.InvestigationGraph {
	now := time.Now().UTC()
	return contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Investigation: contracts.Investigation{
				ID:             "inv-001",
				TenantID:       "tenant-1",
				Status:         contracts.StatusAnalyzing,
				AlertIDs:       []string{"alert-001"},
				Targets:        []contracts.TargetRef{{Kind: "k8s.deployment", Namespace: "default", Name: "api-server"}},
				TimeFrom:       now.Add(-1 * time.Hour),
				TimeTo:         now,
				SlackChannelID: "C12345",
				SlackThreadTS:  "1234567890.123456",
			},
			Alerts: []contracts.NormalizedAlert{
				{
					ID:       "alert-001",
					Source:   "alertmanager",
					TenantID: "tenant-1",
					Status:   contracts.AlertStatusFiring,
					Severity: contracts.SeverityCritical,
					Title:    "Pod OOMKilled",
					Summary:  "api-server pod was OOMKilled",
					StartsAt: now.Add(-30 * time.Minute),
					Labels: map[string]string{
						"service":   "api-server",
						"namespace": "default",
						"cluster":   "prod-us-east-1",
					},
				},
			},
			Evidence: []contracts.Evidence{
				{
					ID:         "ev-001",
					Kind:       contracts.EvidenceLog,
					Source:     "loki",
					Summary:    "OOMKilled: container exceeded memory limit",
					Attributes: map[string]string{"sub_kind": "error"},
				},
				{
					ID:         "ev-002",
					Kind:       contracts.EvidenceK8sState,
					Source:     "kubernetes",
					Summary:    "Pod restarting: exit code 137",
					Attributes: map[string]string{"sub_kind": "pod_status"},
				},
			},
		},
	}
}

func testGraphNoSlack() contracts.InvestigationGraph {
	g := testGraph()
	g.Data.Investigation.SlackChannelID = ""
	g.Data.Investigation.SlackThreadTS = ""
	return g
}

func newTestFollowUpExecutor(evidence []contracts.Evidence) *llm.FollowUpExecutor {
	return llm.NewFollowUpExecutor(
		&mockFollowUpCollectorSet{evidence: evidence},
		nil,
		zap.NewNop(),
	)
}

func deepCollectorEvidence() []contracts.Evidence {
	return []contracts.Evidence{
		{
			ID:         "ev-deep-001",
			Kind:       contracts.EvidenceLog,
			Source:     "loki",
			Summary:    "trace abc123: processRequest allocating 2GB",
			Attributes: map[string]string{"sub_kind": "current"},
		},
		{
			ID:         "ev-deep-002",
			Kind:       contracts.EvidenceEvent,
			Source:     "kubernetes",
			Summary:    "Pod restarted due to OOMKilled",
			Attributes: map[string]string{},
		},
	}
}

// --- LLM Response Fixtures ---

const llmResponseNoFollowUp = `SUMMARY: Pod crashed due to OOM
ROOT_CAUSE: Memory leak in handler function
SEVERITY: high
EXIT_TYPE: oom
ACTION_REQUIRED: yes
BUG_FIXABLE: yes
CONFIDENCE: high
RECOMMENDATIONS:
- Increase memory limit
- Fix the memory leak`

const llmResponseWithFollowUp = `SUMMARY: Pod crashed due to OOM (initial assessment)
ROOT_CAUSE: Suspected memory leak in handler function
SEVERITY: high
EXIT_TYPE: oom
ACTION_REQUIRED: yes
BUG_FIXABLE: yes
CONFIDENCE: medium
RECOMMENDATIONS:
- Increase memory limit
- Investigate handler code
FOLLOW_UP:
- TRACE_LOGS: abc123
- POD_EVENTS: all`

const llmResponsePass2HighConf = `SUMMARY: Memory leak in processRequest handler confirmed
ROOT_CAUSE: Unbounded slice growth in processRequest leads to OOM
SEVERITY: high
EXIT_TYPE: oom
ACTION_REQUIRED: yes
BUG_FIXABLE: yes
CONFIDENCE: high
RECOMMENDATIONS:
- Fix unbounded slice in processRequest
- Add memory limits`

const llmResponsePass2HighConfWithFollowUps = `SUMMARY: Root cause confirmed - memory leak in handler
ROOT_CAUSE: Unbounded slice growth in processRequest
SEVERITY: high
EXIT_TYPE: oom
ACTION_REQUIRED: yes
BUG_FIXABLE: yes
CONFIDENCE: high
RECOMMENDATIONS:
- Fix the slice growth
FOLLOW_UP:
- GITHUB_FILES: internal/handler/process.go`

const llmResponseLowConfWithFollowUps = `SUMMARY: Investigating OOM issue
ROOT_CAUSE: Unknown - need more data
SEVERITY: high
EXIT_TYPE: oom
ACTION_REQUIRED: yes
BUG_FIXABLE: unknown
CONFIDENCE: low
RECOMMENDATIONS:
- Continue investigation
FOLLOW_UP:
- TRACE_LOGS: trace1
- POD_EVENTS: all`

// --- Tests ---

func TestLLMEngine_Name(t *testing.T) {
	engine := NewLLMEngine(
		&mockLLMProvider{nameVal: "test"},
		nil,
		&mockFallbackEngine{},
		LLMEngineConfig{},
		zap.NewNop(),
	)

	if got := engine.Name(); got != "llm-powered" {
		t.Errorf("Name() = %q, want %q", got, "llm-powered")
	}
}

func TestLLMEngine_SinglePass_NoFollowUps(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{
				Content:      llmResponseNoFollowUp,
				InputTokens:  100,
				OutputTokens: 50,
				Model:        "test-model",
				Latency:      500 * time.Millisecond,
			},
		},
	}
	fallback := &mockFallbackEngine{
		hypotheses: []contracts.Hypothesis{{Title: "fallback"}},
	}
	engine := NewLLMEngine(provider, nil, fallback, LLMEngineConfig{MaxPasses: 3}, zap.NewNop())

	hypotheses, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}

	if len(provider.calls) != 1 {
		t.Errorf("expected 1 LLM call, got %d", len(provider.calls))
	}
	if fallback.called {
		t.Error("fallback should not have been called")
	}
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.ID == "" {
		t.Error("hypothesis ID should be set")
	}
	if h.Title != "Pod crashed due to OOM" {
		t.Errorf("Title = %q, want %q", h.Title, "Pod crashed due to OOM")
	}
	if h.Narrative != "Memory leak in handler function" {
		t.Errorf("Narrative = %q, want %q", h.Narrative, "Memory leak in handler function")
	}
	if h.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", h.Confidence)
	}
	if h.CauseCategory != contracts.CauseCapacity {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseCapacity)
	}
	if len(h.Supporting) != 2 {
		t.Errorf("Supporting count = %d, want 2", len(h.Supporting))
	}
	if len(h.SuggestedFixes) != 2 {
		t.Errorf("SuggestedFixes count = %d, want 2", len(h.SuggestedFixes))
	}
}

func TestLLMEngine_MultiPass_WithFollowUps(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{Content: llmResponseWithFollowUp, InputTokens: 200, OutputTokens: 100, Model: "test-model", Latency: time.Second},
			{Content: llmResponsePass2HighConf, InputTokens: 400, OutputTokens: 150, Model: "test-model", Latency: 2 * time.Second},
		},
	}
	fallback := &mockFallbackEngine{}
	engine := NewLLMEngine(
		provider,
		newTestFollowUpExecutor(deepCollectorEvidence()),
		fallback,
		LLMEngineConfig{MaxPasses: 3},
		zap.NewNop(),
	)

	hypotheses, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}

	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 LLM calls (2 passes), got %d", len(provider.calls))
	}
	if fallback.called {
		t.Error("fallback should not have been called")
	}
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Title != "Memory leak in processRequest handler confirmed" {
		t.Errorf("Title = %q, want pass 2 summary", h.Title)
	}
	if h.Narrative != "Unbounded slice growth in processRequest leads to OOM" {
		t.Errorf("Narrative = %q, want pass 2 root cause", h.Narrative)
	}
	if h.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", h.Confidence)
	}
}

func TestLLMEngine_MaxPasses_Respected(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{Content: llmResponseLowConfWithFollowUps},
			{Content: llmResponseLowConfWithFollowUps},
			{Content: llmResponseLowConfWithFollowUps}, // should not be reached
		},
	}
	fallback := &mockFallbackEngine{}
	engine := NewLLMEngine(
		provider,
		newTestFollowUpExecutor(deepCollectorEvidence()),
		fallback,
		LLMEngineConfig{MaxPasses: 2},
		zap.NewNop(),
	)

	_, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}

	if len(provider.calls) != 2 {
		t.Errorf("expected 2 LLM calls (MaxPasses=2), got %d", len(provider.calls))
	}
}

func TestLLMEngine_HighConfidence_SkipsPass3(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{Content: llmResponseWithFollowUp},              // pass 1: medium confidence + follow-ups
			{Content: llmResponsePass2HighConfWithFollowUps}, // pass 2: high confidence + follow-ups
			{Content: llmResponseNoFollowUp},                 // pass 3: should not be reached
		},
	}
	fallback := &mockFallbackEngine{}
	engine := NewLLMEngine(
		provider,
		newTestFollowUpExecutor(deepCollectorEvidence()),
		fallback,
		LLMEngineConfig{MaxPasses: 4},
		zap.NewNop(),
	)

	hypotheses, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}

	if len(provider.calls) != 2 {
		t.Errorf("expected 2 LLM calls (pass 3 skipped due to high confidence), got %d", len(provider.calls))
	}
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}
	if hypotheses[0].Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", hypotheses[0].Confidence)
	}
}

func TestLLMEngine_FallbackOnLLMError(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		errors:  []error{fmt.Errorf("API rate limited")},
	}
	fallback := &mockFallbackEngine{
		hypotheses: []contracts.Hypothesis{
			{ID: "fb-001", Title: "Fallback hypothesis", Confidence: 0.5},
		},
	}
	engine := NewLLMEngine(provider, nil, fallback, LLMEngineConfig{MaxPasses: 3}, zap.NewNop())

	hypotheses, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() should succeed via fallback, got error: %v", err)
	}
	if !fallback.called {
		t.Error("fallback engine should have been called")
	}
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis from fallback, got %d", len(hypotheses))
	}
	if hypotheses[0].Title != "Fallback hypothesis" {
		t.Errorf("Title = %q, want %q", hypotheses[0].Title, "Fallback hypothesis")
	}
}

func TestLLMEngine_FallbackOnEmptyHypotheses(t *testing.T) {
	// When the LLM returns unparseable garbage, the analysis has all fields empty.
	// MapToHypotheses should produce no meaningful hypotheses, triggering the
	// fallback path at: if len(hypotheses) == 0 { return e.fallback.Rank(...) }
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{Content: "completely unparseable garbage with no structure whatsoever"},
		},
	}
	fallback := &mockFallbackEngine{
		hypotheses: []contracts.Hypothesis{
			{ID: "fb-001", Title: "Fallback hypothesis", Confidence: 0.5},
		},
	}
	engine := NewLLMEngine(provider, nil, fallback, LLMEngineConfig{MaxPasses: 3}, zap.NewNop())

	hypotheses, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() returned unexpected error: %v", err)
	}
	if !fallback.called {
		t.Error("expected fallback engine to be called when LLM produces no meaningful hypotheses")
	}
	if len(hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis from fallback")
	}
	if hypotheses[0].Title != "Fallback hypothesis" {
		t.Errorf("Title = %q, want %q", hypotheses[0].Title, "Fallback hypothesis")
	}
}

func TestLLMEngine_NotifierCalled(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{Content: llmResponseWithFollowUp},
			{Content: llmResponsePass2HighConf},
		},
	}
	notifier := &mockPassNotifier{}
	engine := NewLLMEngine(
		provider,
		newTestFollowUpExecutor(deepCollectorEvidence()),
		&mockFallbackEngine{},
		LLMEngineConfig{MaxPasses: 3},
		zap.NewNop(),
	)
	engine.SetNotifier(notifier)

	_, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}

	if len(notifier.notifications) == 0 {
		t.Fatal("expected notifier to receive at least one notification")
	}
	// 2-pass run: pass 1 start (1) + pass 2 follow-up (1) + pass 2 deep analysis (1) = 3
	if len(notifier.notifications) != 3 {
		t.Errorf("notification count = %d, want 3 (for 2-pass analysis)", len(notifier.notifications))
	}
}

func TestLLMEngine_NotifierNotCalled_NoChannel(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{Content: llmResponseNoFollowUp},
		},
	}
	notifier := &mockPassNotifier{}
	engine := NewLLMEngine(provider, nil, &mockFallbackEngine{}, LLMEngineConfig{MaxPasses: 3}, zap.NewNop())
	engine.SetNotifier(notifier)

	_, err := engine.Rank(context.Background(), testGraphNoSlack())
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}

	if len(notifier.notifications) != 0 {
		t.Errorf("expected 0 notifications without SlackChannelID, got %d", len(notifier.notifications))
	}
}

func TestLLMEngine_DefaultMaxPasses(t *testing.T) {
	provider := &mockLLMProvider{
		nameVal: "test-provider",
		responses: []*llm.CompletionResponse{
			{Content: llmResponseLowConfWithFollowUps},
			{Content: llmResponseLowConfWithFollowUps},
			{Content: llmResponseLowConfWithFollowUps},
			{Content: llmResponseLowConfWithFollowUps}, // should not be reached
		},
	}
	engine := NewLLMEngine(
		provider,
		newTestFollowUpExecutor(deepCollectorEvidence()),
		&mockFallbackEngine{},
		LLMEngineConfig{MaxPasses: 0}, // should default to 3
		zap.NewNop(),
	)

	_, err := engine.Rank(context.Background(), testGraph())
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}

	if len(provider.calls) != 3 {
		t.Errorf("expected 3 LLM calls (default MaxPasses=3), got %d", len(provider.calls))
	}
}

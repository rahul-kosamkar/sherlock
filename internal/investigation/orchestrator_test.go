package investigation

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

// --- Mocks ---

type mockInvestigationStore struct {
	created []*contracts.Investigation
	err     error
}

func (m *mockInvestigationStore) Create(ctx context.Context, inv *contracts.Investigation) error {
	m.created = append(m.created, inv)
	return m.err
}

func (m *mockInvestigationStore) GetByID(ctx context.Context, id string) (*contracts.Investigation, error) {
	return nil, nil
}

func (m *mockInvestigationStore) UpdateStatus(ctx context.Context, id string, status contracts.InvestigationStatus) error {
	return nil
}

func (m *mockInvestigationStore) Complete(ctx context.Context, id string, headline string, confidence float64) error {
	return nil
}

type mockEvidenceStore struct{}

func (m *mockEvidenceStore) CreateBatch(ctx context.Context, evidence []contracts.Evidence) error {
	return nil
}

func (m *mockEvidenceStore) ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.Evidence, error) {
	return nil, nil
}

type mockTimelineStore struct{}

func (m *mockTimelineStore) CreateBatch(ctx context.Context, events []contracts.TimelineEvent) error {
	return nil
}

type mockHypothesisStore struct {
	saved []contracts.Hypothesis
}

func (m *mockHypothesisStore) CreateBatch(ctx context.Context, investigationID string, hypotheses []contracts.Hypothesis) error {
	m.saved = hypotheses
	return nil
}

type mockAlertStore struct{}

func (m *mockAlertStore) Create(ctx context.Context, alert *contracts.NormalizedAlert) error {
	return nil
}

func (m *mockAlertStore) GetByID(ctx context.Context, id string) (*contracts.NormalizedAlert, error) {
	return nil, nil
}

type mockCollectorSet struct {
	evidence []contracts.Evidence
}

func (m *mockCollectorSet) CollectAll(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	return m.evidence, nil
}

type mockCorrelatorService struct{}

func (m *mockCorrelatorService) Correlate(ctx context.Context, data contracts.InvestigationData) (contracts.InvestigationGraph, error) {
	return contracts.InvestigationGraph{Data: data}, nil
}

type mockRCAService struct {
	name       string
	hypotheses []contracts.Hypothesis
	err        error
	called     bool
}

func (m *mockRCAService) Name() string { return m.name }

func (m *mockRCAService) Rank(ctx context.Context, graph contracts.InvestigationGraph) ([]contracts.Hypothesis, error) {
	m.called = true
	return m.hypotheses, m.err
}

type mockTimelineBuilder struct{}

func (m *mockTimelineBuilder) Build(data contracts.InvestigationData) []contracts.TimelineEvent {
	return nil
}

type mockEntityResolver struct{}

func (m *mockEntityResolver) Resolve(alert *contracts.NormalizedAlert) EntityResolveResult {
	return EntityResolveResult{
		Targets:  []contracts.TargetRef{{Kind: "k8s.deployment", Name: "test-svc"}},
		TimeFrom: time.Now().UTC().Add(-1 * time.Hour),
		TimeTo:   time.Now().UTC(),
	}
}

type mockSlackNotifier struct {
	results []*contracts.InvestigationResult
}

func (m *mockSlackNotifier) PostInvestigationStarted(ctx context.Context, channelID string, investigationID string) (string, error) {
	return "thread-ts-001", nil
}

func (m *mockSlackNotifier) PostEvidenceUpdate(ctx context.Context, channelID, threadTS string, message string) error {
	return nil
}

func (m *mockSlackNotifier) PostResult(ctx context.Context, channelID, threadTS string, result *contracts.InvestigationResult) error {
	m.results = append(m.results, result)
	return nil
}

func (m *mockSlackNotifier) PostError(ctx context.Context, channelID, threadTS string, errMsg string) error {
	return nil
}

type mockAuditLogger struct{}

func (m *mockAuditLogger) Log(ctx context.Context, tenantID, actor, action, target string, metadata map[string]string) error {
	return nil
}

type mockQueueConsumer struct{}

func (m *mockQueueConsumer) Subscribe(ctx context.Context, subject string, handler func(msg []byte, ack func(), nak func()) error) error {
	return nil
}

// --- Helpers ---

func baseDeps(rcaSvc *mockRCAService, slack SlackNotifier) OrchestratorDeps {
	return OrchestratorDeps{
		Investigations: &mockInvestigationStore{},
		Evidence:       &mockEvidenceStore{},
		Timelines:      &mockTimelineStore{},
		Hypotheses:     &mockHypothesisStore{},
		Alerts:         &mockAlertStore{},
		Collectors:     &mockCollectorSet{},
		Correlator:     &mockCorrelatorService{},
		RCA:            rcaSvc,
		Timeline:       &mockTimelineBuilder{},
		Entity:         &mockEntityResolver{},
		Slack:          slack,
		Audit:          &mockAuditLogger{},
		Consumer:       &mockQueueConsumer{},
		Logger:         zap.NewNop(),
	}
}

func newFullOrchestrator(rcaSvc *mockRCAService, slack SlackNotifier, opts ...Option) *Orchestrator {
	return NewOrchestrator(baseDeps(rcaSvc, slack), opts...)
}

func testJob() contracts.InvestigationJob {
	return contracts.InvestigationJob{
		Alert: contracts.NormalizedAlert{
			ID:       "alert-orch-001",
			TenantID: "tenant-1",
			Status:   contracts.AlertStatusFiring,
			Severity: contracts.SeverityCritical,
			Title:    "Test Alert",
			Summary:  "Something went wrong",
			StartsAt: time.Now().UTC().Add(-15 * time.Minute),
			Labels:   map[string]string{"service": "api"},
		},
		SlackChannelID: "C-test-channel",
	}
}

// --- Tests ---

func TestOrchestrator_SelectsRuleBased_WhenLLMDisabled(t *testing.T) {
	ruleBased := &mockRCAService{name: "rule-based"}
	llmEngine := &mockRCAService{name: "llm-powered"}

	o := &Orchestrator{
		rca:        ruleBased,
		llmRCA:     llmEngine,
		llmEnabled: false,
	}

	engine := o.selectRCAEngine()
	if engine.Name() != "rule-based" {
		t.Errorf("selectRCAEngine() = %q, want %q", engine.Name(), "rule-based")
	}
}

func TestOrchestrator_SelectsLLM_WhenEnabled(t *testing.T) {
	ruleBased := &mockRCAService{name: "rule-based"}
	llmEngine := &mockRCAService{name: "llm-powered"}

	o := NewOrchestrator(baseDeps(ruleBased, &mockSlackNotifier{}), WithLLMEngine(llmEngine))

	engine := o.selectRCAEngine()
	if engine.Name() != "llm-powered" {
		t.Errorf("selectRCAEngine() = %q, want %q", engine.Name(), "llm-powered")
	}
}

func TestOrchestrator_SelectsRuleBased_WhenLLMNil(t *testing.T) {
	ruleBased := &mockRCAService{name: "rule-based"}

	o := &Orchestrator{
		rca:        ruleBased,
		llmEnabled: true,
	}

	engine := o.selectRCAEngine()
	if engine.Name() != "rule-based" {
		t.Errorf("selectRCAEngine() = %q, want %q when llmRCA is nil", engine.Name(), "rule-based")
	}
}

func TestOrchestrator_ResultIncludesRCAEngine(t *testing.T) {
	llmRCA := &mockRCAService{
		name: "llm-powered",
		hypotheses: []contracts.Hypothesis{
			{ID: "h-1", Title: "LLM hypothesis", Confidence: 0.9},
		},
	}
	slack := &mockSlackNotifier{}
	ruleBased := &mockRCAService{name: "rule-based"}

	o := newFullOrchestrator(ruleBased, slack, WithLLMEngine(llmRCA))

	err := o.runInvestigation(context.Background(), testJob())
	if err != nil {
		t.Fatalf("runInvestigation() error: %v", err)
	}

	if len(slack.results) != 1 {
		t.Fatalf("expected 1 result posted to Slack, got %d", len(slack.results))
	}

	result := slack.results[0]
	if result.RCAEngine != "llm-powered" {
		t.Errorf("RCAEngine = %q, want %q", result.RCAEngine, "llm-powered")
	}
}

func TestOrchestrator_ResultIncludesRootCause(t *testing.T) {
	rcaSvc := &mockRCAService{
		name: "rule-based",
		hypotheses: []contracts.Hypothesis{
			{
				ID:         "h-1",
				Title:      "OOM in api-server",
				Narrative:  "Memory leak caused by unbounded cache growth in processRequest handler",
				Confidence: 0.8,
			},
		},
	}
	slack := &mockSlackNotifier{}

	o := newFullOrchestrator(rcaSvc, slack)

	err := o.runInvestigation(context.Background(), testJob())
	if err != nil {
		t.Fatalf("runInvestigation() error: %v", err)
	}

	if len(slack.results) != 1 {
		t.Fatalf("expected 1 result posted to Slack, got %d", len(slack.results))
	}

	result := slack.results[0]
	want := "Memory leak caused by unbounded cache growth in processRequest handler"
	if result.RootCause != want {
		t.Errorf("RootCause = %q, want %q", result.RootCause, want)
	}
}

// --- Mock Publisher ---

type mockQueuePublisher struct {
	subject string
	data    []byte
	err     error
}

func (m *mockQueuePublisher) Publish(ctx context.Context, subject string, data []byte) error {
	m.subject = subject
	m.data = data
	return m.err
}

// --- Mock Slack Notifier with error tracking ---

type mockSlackNotifierWithErrors struct {
	results   []*contracts.InvestigationResult
	errors    []string
	startedID string
}

func (m *mockSlackNotifierWithErrors) PostInvestigationStarted(ctx context.Context, channelID string, investigationID string) (string, error) {
	m.startedID = investigationID
	return "thread-ts-001", nil
}

func (m *mockSlackNotifierWithErrors) PostEvidenceUpdate(ctx context.Context, channelID, threadTS string, message string) error {
	return nil
}

func (m *mockSlackNotifierWithErrors) PostResult(ctx context.Context, channelID, threadTS string, result *contracts.InvestigationResult) error {
	m.results = append(m.results, result)
	return nil
}

func (m *mockSlackNotifierWithErrors) PostError(ctx context.Context, channelID, threadTS string, errMsg string) error {
	m.errors = append(m.errors, errMsg)
	return nil
}

// --- Additional Tests ---

func TestNewOrchestrator_DefaultMaxConcurrent(t *testing.T) {
	o := newFullOrchestrator(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	if o.maxConcurrent != 10 {
		t.Errorf("maxConcurrent = %d, want 10", o.maxConcurrent)
	}
}

func TestNewOrchestrator_DefaultTimeout(t *testing.T) {
	o := newFullOrchestrator(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	want := 5 * time.Minute
	if o.timeout != want {
		t.Errorf("timeout = %v, want %v", o.timeout, want)
	}
}

func TestNewOrchestrator_CustomValues(t *testing.T) {
	o := newFullOrchestrator(
		&mockRCAService{name: "rule-based"},
		&mockSlackNotifier{},
		WithMaxConcurrent(5),
		WithTimeout(2*time.Minute),
	)
	if o.maxConcurrent != 5 {
		t.Errorf("maxConcurrent = %d, want 5", o.maxConcurrent)
	}
	if o.timeout != 2*time.Minute {
		t.Errorf("timeout = %v, want %v", o.timeout, 2*time.Minute)
	}
}

func TestRunInvestigation_FullFlow(t *testing.T) {
	rcaSvc := &mockRCAService{
		name: "rule-based",
		hypotheses: []contracts.Hypothesis{
			{ID: "h-1", Title: "OOM Kill", Narrative: "Pod killed by OOM", Confidence: 0.85},
		},
	}
	slack := &mockSlackNotifier{}
	invStore := &mockInvestigationStore{}

	deps := baseDeps(rcaSvc, slack)
	deps.Investigations = invStore
	o := NewOrchestrator(deps)

	err := o.runInvestigation(context.Background(), testJob())
	if err != nil {
		t.Fatalf("runInvestigation() error: %v", err)
	}

	if len(invStore.created) != 1 {
		t.Fatalf("investigation store has %d entries, want 1", len(invStore.created))
	}

	if len(slack.results) != 1 {
		t.Fatalf("expected 1 result posted to Slack, got %d", len(slack.results))
	}

	result := slack.results[0]
	if result.Headline != "OOM Kill" {
		t.Errorf("Headline = %q, want %q", result.Headline, "OOM Kill")
	}
	if result.Confidence != 0.85 {
		t.Errorf("Confidence = %f, want 0.85", result.Confidence)
	}
}

func TestRunInvestigation_NoHypotheses(t *testing.T) {
	rcaSvc := &mockRCAService{
		name:       "rule-based",
		hypotheses: nil,
	}
	slack := &mockSlackNotifier{}

	o := newFullOrchestrator(rcaSvc, slack)

	err := o.runInvestigation(context.Background(), testJob())
	if err != nil {
		t.Fatalf("runInvestigation() error: %v", err)
	}

	if len(slack.results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(slack.results))
	}

	result := slack.results[0]
	if result.Headline != "No clear hypothesis identified" {
		t.Errorf("Headline = %q, want %q", result.Headline, "No clear hypothesis identified")
	}
	if result.Confidence != 0.0 {
		t.Errorf("Confidence = %f, want 0.0", result.Confidence)
	}
}

func TestRunInvestigation_NoSlackChannel(t *testing.T) {
	rcaSvc := &mockRCAService{
		name: "rule-based",
		hypotheses: []contracts.Hypothesis{
			{ID: "h-1", Title: "Disk full", Confidence: 0.7},
		},
	}
	slack := &mockSlackNotifier{}

	o := newFullOrchestrator(rcaSvc, slack)

	job := testJob()
	job.SlackChannelID = ""

	err := o.runInvestigation(context.Background(), job)
	if err != nil {
		t.Fatalf("runInvestigation() error: %v", err)
	}

	if len(slack.results) != 0 {
		t.Errorf("expected 0 results posted to Slack for empty channel, got %d", len(slack.results))
	}
}

func TestRunInvestigation_RCAError(t *testing.T) {
	rcaSvc := &mockRCAService{
		name: "rule-based",
		err:  fmt.Errorf("model unavailable"),
	}
	slackNotif := &mockSlackNotifierWithErrors{}

	o := NewOrchestrator(baseDeps(rcaSvc, slackNotif))

	err := o.runInvestigation(context.Background(), testJob())
	if err == nil {
		t.Fatal("expected error from runInvestigation when RCA fails")
	}

	if len(slackNotif.errors) != 1 {
		t.Fatalf("expected 1 error posted to Slack, got %d", len(slackNotif.errors))
	}
}

func TestRunInvestigation_InvestigationCreateError(t *testing.T) {
	invStore := &mockInvestigationStore{err: fmt.Errorf("db connection refused")}

	deps := baseDeps(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	deps.Investigations = invStore
	o := NewOrchestrator(deps)

	err := o.runInvestigation(context.Background(), testJob())
	if err == nil {
		t.Fatal("expected error when investigation store Create fails")
	}
}

func TestEnqueueInvestigation_Success(t *testing.T) {
	pub := &mockQueuePublisher{}
	alert := contracts.NormalizedAlert{
		ID:       "alert-enq-1",
		TenantID: "tenant-1",
		Title:    "High latency",
	}

	id, err := EnqueueInvestigation(context.Background(), pub, "investigations", alert, "C-chan", "ts-1", "U-user")
	if err != nil {
		t.Fatalf("EnqueueInvestigation() error: %v", err)
	}

	if id != "alert-enq-1" {
		t.Errorf("returned ID = %q, want %q", id, "alert-enq-1")
	}

	if pub.subject != "investigations.new" {
		t.Errorf("published subject = %q, want %q", pub.subject, "investigations.new")
	}

	var job contracts.InvestigationJob
	if err := json.Unmarshal(pub.data, &job); err != nil {
		t.Fatalf("failed to unmarshal published data: %v", err)
	}
	if job.Alert.ID != "alert-enq-1" {
		t.Errorf("job alert ID = %q, want %q", job.Alert.ID, "alert-enq-1")
	}
	if job.SlackChannelID != "C-chan" {
		t.Errorf("job SlackChannelID = %q, want %q", job.SlackChannelID, "C-chan")
	}
}

func TestEnqueueInvestigation_EmptyAlertID(t *testing.T) {
	pub := &mockQueuePublisher{}
	alert := contracts.NormalizedAlert{
		ID:    "",
		Title: "No ID alert",
	}

	id, err := EnqueueInvestigation(context.Background(), pub, "investigations", alert, "C-chan", "", "")
	if err != nil {
		t.Fatalf("EnqueueInvestigation() error: %v", err)
	}

	if id == "" {
		t.Error("expected non-empty ID when alert ID was empty (should generate UUID)")
	}
}

func TestEnqueueInvestigation_PublishError(t *testing.T) {
	pub := &mockQueuePublisher{err: fmt.Errorf("connection timeout")}
	alert := contracts.NormalizedAlert{
		ID:    "alert-fail-1",
		Title: "Will fail",
	}

	_, err := EnqueueInvestigation(context.Background(), pub, "investigations", alert, "C-chan", "", "")
	if err == nil {
		t.Fatal("expected error when publisher fails")
	}
}

func TestFailInvestigation_PostsErrorToSlack(t *testing.T) {
	slackNotif := &mockSlackNotifierWithErrors{}
	o := newFullOrchestrator(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	o.slack = slackNotif

	inv := &contracts.Investigation{
		ID:             "inv-fail-1",
		SlackChannelID: "C-test",
		SlackThreadTS:  "ts-1",
	}

	o.failInvestigation(context.Background(), inv, "something went wrong")

	if len(slackNotif.errors) != 1 {
		t.Fatalf("expected 1 error posted to Slack, got %d", len(slackNotif.errors))
	}
	if slackNotif.errors[0] != "something went wrong" {
		t.Errorf("error message = %q, want %q", slackNotif.errors[0], "something went wrong")
	}
}

func TestFailInvestigation_NoSlackChannel(t *testing.T) {
	slackNotif := &mockSlackNotifierWithErrors{}
	o := newFullOrchestrator(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	o.slack = slackNotif

	inv := &contracts.Investigation{
		ID:             "inv-fail-2",
		SlackChannelID: "",
	}

	err := o.failInvestigation(context.Background(), inv, "no slack channel")
	if err == nil {
		t.Fatal("expected error from failInvestigation")
	}
	if len(slackNotif.errors) != 0 {
		t.Errorf("expected 0 errors posted to Slack (no channel), got %d", len(slackNotif.errors))
	}
}

func TestRunInvestigation_WithCollectedEvidence(t *testing.T) {
	evidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Summary: "CrashLoopBackOff"},
		{ID: "ev-2", Kind: contracts.EvidenceLog, Summary: "error logs"},
	}
	rcaSvc := &mockRCAService{
		name: "rule-based",
		hypotheses: []contracts.Hypothesis{
			{ID: "h-1", Title: "Container crash", Confidence: 0.8, Narrative: "CrashLoop detected"},
		},
	}
	slack := &mockSlackNotifier{}
	hypStore := &mockHypothesisStore{}

	deps := baseDeps(rcaSvc, slack)
	deps.Hypotheses = hypStore
	deps.Collectors = &mockCollectorSet{evidence: evidence}
	o := NewOrchestrator(deps)

	err := o.runInvestigation(context.Background(), testJob())
	if err != nil {
		t.Fatalf("runInvestigation() error: %v", err)
	}

	if len(hypStore.saved) != 1 {
		t.Fatalf("expected 1 hypothesis saved, got %d", len(hypStore.saved))
	}

	if len(slack.results) != 1 {
		t.Fatalf("expected 1 Slack result, got %d", len(slack.results))
	}

	result := slack.results[0]
	if result.RootCause != "CrashLoop detected" {
		t.Errorf("RootCause = %q, want %q", result.RootCause, "CrashLoop detected")
	}
	if len(result.RecommendedActions) != 0 {
		t.Errorf("expected 0 recommended actions (none in hypothesis), got %d", len(result.RecommendedActions))
	}
}

func TestRunInvestigation_WithSuggestedFixes(t *testing.T) {
	rcaSvc := &mockRCAService{
		name: "rule-based",
		hypotheses: []contracts.Hypothesis{
			{
				ID:         "h-1",
				Title:      "Memory leak",
				Confidence: 0.9,
				Narrative:  "Unbounded cache growth",
				SuggestedFixes: []contracts.SuggestedFix{
					{Title: "Increase limits", Description: "Bump memory limits", SafeByDefault: false},
				},
			},
		},
	}
	slack := &mockSlackNotifier{}

	o := newFullOrchestrator(rcaSvc, slack)

	err := o.runInvestigation(context.Background(), testJob())
	if err != nil {
		t.Fatalf("runInvestigation() error: %v", err)
	}

	result := slack.results[0]
	if len(result.RecommendedActions) != 1 {
		t.Fatalf("expected 1 recommended action, got %d", len(result.RecommendedActions))
	}
	if result.RecommendedActions[0].Title != "Increase limits" {
		t.Errorf("action title = %q, want %q", result.RecommendedActions[0].Title, "Increase limits")
	}
}

func TestRunInvestigation_NilSlackNotifier(t *testing.T) {
	rcaSvc := &mockRCAService{
		name: "rule-based",
		hypotheses: []contracts.Hypothesis{
			{ID: "h-1", Title: "Test", Confidence: 0.5},
		},
	}

	deps := baseDeps(rcaSvc, nil)
	o := NewOrchestrator(deps)

	job := testJob()
	job.SlackChannelID = "C-test"

	err := o.runInvestigation(context.Background(), job)
	if err != nil {
		t.Fatalf("runInvestigation() with nil slack should not error: %v", err)
	}
}

func TestWithLLMEngine(t *testing.T) {
	o := newFullOrchestrator(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})

	if o.llmRCA != nil {
		t.Fatal("llmRCA should be nil before WithLLMEngine")
	}

	llmSvc := &mockRCAService{name: "llm-powered"}
	o2 := NewOrchestrator(baseDeps(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{}), WithLLMEngine(llmSvc))

	if o2.llmRCA == nil {
		t.Fatal("llmRCA should not be nil after WithLLMEngine")
	}
	if o2.llmRCA.Name() != "llm-powered" {
		t.Errorf("llmRCA.Name() = %q, want %q", o2.llmRCA.Name(), "llm-powered")
	}
	if !o2.llmEnabled {
		t.Error("llmEnabled should be true after WithLLMEngine")
	}
}

type mockQueueConsumerCapture struct {
	subject string
	handler func(msg []byte, ack func(), nak func()) error
	err     error
}

func (m *mockQueueConsumerCapture) Subscribe(_ context.Context, subject string, handler func(msg []byte, ack func(), nak func()) error) error {
	m.subject = subject
	m.handler = handler
	return m.err
}

func TestStart_SubscribesCorrectSubject(t *testing.T) {
	consumer := &mockQueueConsumerCapture{}
	deps := baseDeps(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	deps.Consumer = consumer
	o := NewOrchestrator(deps)

	err := o.Start(context.Background(), "investigations")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if consumer.subject != "investigations.new" {
		t.Errorf("subscribed subject = %q, want %q", consumer.subject, "investigations.new")
	}
	if consumer.handler == nil {
		t.Fatal("handler should not be nil")
	}
}

func TestStart_SubscribeError(t *testing.T) {
	consumer := &mockQueueConsumerCapture{err: fmt.Errorf("subscribe failed")}
	deps := baseDeps(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	deps.Consumer = consumer
	o := NewOrchestrator(deps)

	err := o.Start(context.Background(), "investigations")
	if err == nil {
		t.Fatal("expected error when subscribe fails")
	}
}

func TestStart_HandlerProcessesJob(t *testing.T) {
	consumer := &mockQueueConsumerCapture{}
	rcaSvc := &mockRCAService{
		name: "rule-based",
		hypotheses: []contracts.Hypothesis{
			{ID: "h-1", Title: "Test", Confidence: 0.7},
		},
	}
	deps := baseDeps(rcaSvc, &mockSlackNotifier{})
	deps.Consumer = consumer
	o := NewOrchestrator(deps)

	err := o.Start(context.Background(), "investigations")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	job := testJob()
	data, _ := json.Marshal(job)
	acked := false
	nacked := false
	err = consumer.handler(data, func() { acked = true }, func() { nacked = true })
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	o.Stop()
	if !acked {
		t.Error("expected ack to be called on success")
	}
	if nacked {
		t.Error("expected nak NOT to be called on success")
	}
}

func TestStart_HandlerInvalidJSON(t *testing.T) {
	consumer := &mockQueueConsumerCapture{}
	deps := baseDeps(&mockRCAService{name: "rule-based"}, &mockSlackNotifier{})
	deps.Consumer = consumer
	o := NewOrchestrator(deps)

	err := o.Start(context.Background(), "investigations")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	err = consumer.handler([]byte("not valid json"), func() {}, func() {})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

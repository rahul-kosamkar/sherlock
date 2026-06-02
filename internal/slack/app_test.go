package slack

import (
	"context"
	"errors"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/slack/transport"
	"go.uber.org/zap"
)

type mockEnqueuer struct {
	channelID string
	threadTS  string
	userID    string
	text      string
	invID     string
	err       error
}

func (m *mockEnqueuer) EnqueueInvestigation(_ context.Context, channelID, threadTS, userID, text string) (string, error) {
	m.channelID = channelID
	m.threadTS = threadTS
	m.userID = userID
	m.text = text
	return m.invID, m.err
}

type mockPublisher struct {
	startedChannel string
	startedInvID   string
	startedTS      string
	evidenceMsg    string
	errorMsg       string
	err            error
}

func (m *mockPublisher) PostInvestigationStarted(_ context.Context, channelID string, investigationID string) (string, error) {
	m.startedChannel = channelID
	m.startedInvID = investigationID
	m.startedTS = "thread-ts-mock"
	return m.startedTS, m.err
}

func (m *mockPublisher) PostEvidenceUpdate(_ context.Context, _, _ string, msg string) error {
	m.evidenceMsg = msg
	return nil
}

func (m *mockPublisher) PostResult(_ context.Context, _, _ string, _ interface{}) error {
	return nil
}

func (m *mockPublisher) PostError(_ context.Context, _, _ string, errMsg string) error {
	m.errorMsg = errMsg
	return nil
}

type mockEvidenceQuerier struct {
	evidence []contracts.Evidence
	err      error
}

func (m *mockEvidenceQuerier) ListByInvestigation(_ context.Context, _ string) ([]contracts.Evidence, error) {
	return m.evidence, m.err
}

type mockAlertQuerier struct {
	alert *contracts.NormalizedAlert
	err   error
}

func (m *mockAlertQuerier) GetByID(_ context.Context, _ string) (*contracts.NormalizedAlert, error) {
	return m.alert, m.err
}

type mockInvestigationQuerier struct {
	inv *contracts.Investigation
	err error
}

func (m *mockInvestigationQuerier) GetByID(_ context.Context, _ string) (*contracts.Investigation, error) {
	return m.inv, m.err
}

func newTestApp(enq *mockEnqueuer, pub *mockPublisher) *App {
	return &App{
		enqueuer:  enq,
		publisher: &Publisher{logger: zap.NewNop()},
		logger:    zap.NewNop(),
	}
}

func TestNewApp_SocketMode(t *testing.T) {
	cfg := AppConfig{
		Mode:     "socket",
		AppToken: "xapp-test",
		BotToken: "xoxb-test",
	}
	app, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}
	if len(app.transports) != 1 {
		t.Errorf("expected 1 transport, got %d", len(app.transports))
	}
}

func TestNewApp_SocketMode_MissingAppToken(t *testing.T) {
	cfg := AppConfig{
		Mode:     "socket",
		BotToken: "xoxb-test",
	}
	_, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("NewApp() expected error for missing app_token")
	}
}

func TestNewApp_HTTPMode(t *testing.T) {
	cfg := AppConfig{
		Mode:          "http",
		SigningSecret: "secret",
	}
	app, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}
	if len(app.transports) != 1 {
		t.Errorf("expected 1 transport, got %d", len(app.transports))
	}
}

func TestNewApp_HTTPMode_MissingSigningSecret(t *testing.T) {
	cfg := AppConfig{
		Mode: "http",
	}
	_, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("NewApp() expected error for missing signing_secret")
	}
}

func TestNewApp_BothMode(t *testing.T) {
	cfg := AppConfig{
		Mode:          "both",
		AppToken:      "xapp-test",
		BotToken:      "xoxb-test",
		SigningSecret: "secret",
	}
	app, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}
	if len(app.transports) != 2 {
		t.Errorf("expected 2 transports, got %d", len(app.transports))
	}
}

func TestNewApp_BothMode_MissingTokens(t *testing.T) {
	cfg := AppConfig{
		Mode:          "both",
		SigningSecret: "secret",
	}
	_, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("NewApp() expected error for missing tokens in both mode")
	}
}

func TestNewApp_BothMode_MissingSigningSecret(t *testing.T) {
	cfg := AppConfig{
		Mode:     "both",
		AppToken: "xapp-test",
		BotToken: "xoxb-test",
	}
	_, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("NewApp() expected error for missing signing_secret in both mode")
	}
}

func TestNewApp_UnknownMode(t *testing.T) {
	cfg := AppConfig{
		Mode: "unknown",
	}
	_, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("NewApp() expected error for unknown mode")
	}
}

func TestNewApp_HTTPMode_DefaultAddress(t *testing.T) {
	cfg := AppConfig{
		Mode:          "http",
		SigningSecret: "secret",
	}
	_, err := NewApp(cfg, &mockEnqueuer{}, nil, nil, nil, nil, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}
}

func TestHandleSlashCommand_EmptyText(t *testing.T) {
	enq := &mockEnqueuer{invID: "inv-123"}
	app := &App{
		enqueuer: enq,
		logger:   zap.NewNop(),
	}

	err := app.HandleSlashCommand(context.Background(), transport.SlashCommand{
		Command:   "/sherlock",
		Text:      "",
		UserID:    "U123",
		ChannelID: "C123",
	})
	if err != nil {
		t.Fatalf("HandleSlashCommand() error: %v", err)
	}
	if enq.text != "" {
		t.Error("expected enqueue not called for empty text")
	}
}

func TestHandleSlashCommand_Success(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-001"}
	enq := &mockEnqueuer{invID: "inv-200"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleSlashCommand(context.Background(), transport.SlashCommand{
		Command:   "/sherlock",
		Text:      "check payment-api",
		UserID:    "U123",
		ChannelID: "C123",
	})
	if err != nil {
		t.Fatalf("HandleSlashCommand() error: %v", err)
	}
	if enq.text != "check payment-api" {
		t.Errorf("enqueued text = %q, want %q", enq.text, "check payment-api")
	}
	if enq.channelID != "C123" {
		t.Errorf("enqueued channelID = %q, want %q", enq.channelID, "C123")
	}
}

func TestHandleSlashCommand_EnqueueError(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-001"}
	enq := &mockEnqueuer{err: errors.New("queue down")}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleSlashCommand(context.Background(), transport.SlashCommand{
		Command:   "/sherlock",
		Text:      "investigate",
		UserID:    "U123",
		ChannelID: "C123",
	})
	if err == nil {
		t.Fatal("expected error when enqueue fails")
	}
}

func TestHandleSlashCommand_PostStartedError(t *testing.T) {
	mockAPI := &mockSlackAPI{err: errors.New("slack down")}
	enq := &mockEnqueuer{invID: "inv-300"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleSlashCommand(context.Background(), transport.SlashCommand{
		Command:   "/sherlock",
		Text:      "test",
		UserID:    "U123",
		ChannelID: "C123",
	})
	if err == nil {
		t.Fatal("expected error when PostInvestigationStarted fails")
	}
}

func TestHandleMessageShortcut_Success(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-002"}
	enq := &mockEnqueuer{invID: "inv-400"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleMessageShortcut(context.Background(), transport.MessageShortcut{
		CallbackID:  "investigate_message",
		UserID:      "U456",
		ChannelID:   "C456",
		MessageTS:   "1234.5678",
		MessageText: "error: connection refused",
	})
	if err != nil {
		t.Fatalf("HandleMessageShortcut() error: %v", err)
	}
	if enq.text != "error: connection refused" {
		t.Errorf("enqueued text = %q, want %q", enq.text, "error: connection refused")
	}
}

func TestHandleMessageShortcut_EmptyText(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-003"}
	enq := &mockEnqueuer{invID: "inv-500"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleMessageShortcut(context.Background(), transport.MessageShortcut{
		CallbackID:  "investigate_message",
		UserID:      "U789",
		ChannelID:   "C789",
		MessageTS:   "9999.0001",
		MessageText: "",
	})
	if err != nil {
		t.Fatalf("HandleMessageShortcut() error: %v", err)
	}
	if enq.text != "investigate message 9999.0001" {
		t.Errorf("enqueued text = %q, want fallback text", enq.text)
	}
}

func TestHandleMessageShortcut_EnqueueError(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-004"}
	enq := &mockEnqueuer{err: errors.New("enqueue failed")}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleMessageShortcut(context.Background(), transport.MessageShortcut{
		CallbackID:  "investigate_message",
		UserID:      "U100",
		ChannelID:   "C100",
		MessageTS:   "1111.2222",
		MessageText: "test",
	})
	if err == nil {
		t.Fatal("expected error when enqueue fails")
	}
}

func TestHandleInteraction_Rerun_Success(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-005"}
	enq := &mockEnqueuer{invID: "inv-600"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleInteraction(context.Background(), transport.InteractionAction{
		ActionID:  "investigate_rerun",
		Value:     "inv-original",
		UserID:    "U200",
		ChannelID: "C200",
	})
	if err != nil {
		t.Fatalf("HandleInteraction(rerun) error: %v", err)
	}
	if enq.text != "inv-original" {
		t.Errorf("enqueued value = %q, want %q", enq.text, "inv-original")
	}
}

func TestHandleInteraction_Rerun_EnqueueError(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-006"}
	enq := &mockEnqueuer{err: errors.New("rerun failed")}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	app := &App{
		enqueuer:  enq,
		publisher: pub,
		logger:    zap.NewNop(),
	}

	err := app.HandleInteraction(context.Background(), transport.InteractionAction{
		ActionID:  "investigate_rerun",
		Value:     "inv-fail",
		UserID:    "U300",
		ChannelID: "C300",
	})
	if err == nil {
		t.Fatal("expected error when rerun enqueue fails")
	}
}

func TestHandleInteraction_ExpandEvidence(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-010"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	evq := &mockEvidenceQuerier{evidence: []contracts.Evidence{
		{Kind: contracts.EvidenceLog, Summary: "Found error in logs"},
		{Kind: contracts.EvidenceK8sState, Summary: "Pod crashlooping"},
	}}
	app := &App{
		publisher: pub,
		evidence:  evq,
		logger:    zap.NewNop(),
	}
	err := app.HandleInteraction(context.Background(), transport.InteractionAction{
		ActionID:  "investigate_expand_evidence",
		Value:     "inv-evidence-id",
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "1234.5678",
	})
	if err != nil {
		t.Fatalf("HandleInteraction() for expand_evidence should return nil, got: %v", err)
	}
}

func TestHandleInteraction_OpenRunbook(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-011"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	invq := &mockInvestigationQuerier{inv: &contracts.Investigation{
		AlertIDs: []string{"alert-1"},
	}}
	aq := &mockAlertQuerier{alert: &contracts.NormalizedAlert{
		Annotations: map[string]string{"runbook_url": "https://wiki.example.com/runbook"},
	}}
	app := &App{
		publisher:      pub,
		investigations: invq,
		alerts:         aq,
		logger:         zap.NewNop(),
	}
	err := app.HandleInteraction(context.Background(), transport.InteractionAction{
		ActionID:  "investigate_open_runbook",
		Value:     "inv-runbook-id",
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "1234.5678",
	})
	if err != nil {
		t.Fatalf("HandleInteraction() for open_runbook should return nil, got: %v", err)
	}
}

func TestHandleInteraction_OpenRunbook_NoRunbookURL(t *testing.T) {
	mockAPI := &mockSlackAPI{ts: "thread-ts-012"}
	pub := &Publisher{slackClient: mockAPI, logger: zap.NewNop()}
	invq := &mockInvestigationQuerier{inv: &contracts.Investigation{
		AlertIDs: []string{"alert-1"},
	}}
	aq := &mockAlertQuerier{alert: &contracts.NormalizedAlert{
		Annotations: map[string]string{},
	}}
	app := &App{
		publisher:      pub,
		investigations: invq,
		alerts:         aq,
		logger:         zap.NewNop(),
	}
	err := app.HandleInteraction(context.Background(), transport.InteractionAction{
		ActionID:  "investigate_open_runbook",
		Value:     "inv-no-runbook",
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "1234.5678",
	})
	if err != nil {
		t.Fatalf("HandleInteraction() for open_runbook with no URL should return nil, got: %v", err)
	}
}

func TestHandleInteraction_UnknownAction(t *testing.T) {
	app := &App{logger: zap.NewNop()}
	err := app.HandleInteraction(context.Background(), transport.InteractionAction{
		ActionID:  "unknown_action",
		Value:     "value",
		UserID:    "U123",
		ChannelID: "C123",
	})
	if err != nil {
		t.Fatalf("HandleInteraction() for unknown action should return nil, got: %v", err)
	}
}

func TestStop_NoTransports(t *testing.T) {
	app := &App{logger: zap.NewNop()}
	err := app.Stop()
	if err != nil {
		t.Fatalf("Stop() with no transports should return nil, got: %v", err)
	}
}

package transport

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

type mockHandler struct {
	slashCmd    *SlashCommand
	shortcut    *MessageShortcut
	interaction *InteractionAction
	err         error
}

func (m *mockHandler) HandleSlashCommand(_ context.Context, cmd SlashCommand) error {
	m.slashCmd = &cmd
	return m.err
}

func (m *mockHandler) HandleMessageShortcut(_ context.Context, shortcut MessageShortcut) error {
	m.shortcut = &shortcut
	return m.err
}

func (m *mockHandler) HandleInteraction(_ context.Context, action InteractionAction) error {
	m.interaction = &action
	return m.err
}

func (m *mockHandler) HandleAppMention(_ context.Context, _, _, _ string) error {
	return m.err
}

func signRequest(secret, timestamp string, body []byte) string {
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(baseString))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func newTestTransport(h Handler) (*HTTPTransport, string) {
	secret := "test-signing-secret"
	logger := zap.NewNop()
	return NewHTTPTransport(":0", secret, h, logger), secret
}

func TestVerifySlackSignature_Valid(t *testing.T) {
	signingSecret := "test-secret-1234"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte("command=/sherlock&text=test")

	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	signature := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !verifySlackSignature(signingSecret, timestamp, body, signature) {
		t.Error("expected valid signature to return true")
	}
}

func TestVerifySlackSignature_InvalidSignature(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	if verifySlackSignature("secret", ts, []byte("body"), "v0=invalidhex1234567890") {
		t.Error("expected invalid signature to return false")
	}
}

func TestVerifySlackSignature_InvalidTimestamp(t *testing.T) {
	if verifySlackSignature("secret", "not-a-number", []byte("body"), "v0=abc") {
		t.Error("expected non-numeric timestamp to return false")
	}
}

func TestVerifySlackSignature_ExpiredTimestamp(t *testing.T) {
	expired := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	body := []byte("body")
	sig := signRequest("secret", expired, body)
	if verifySlackSignature("secret", expired, body, sig) {
		t.Error("expected expired timestamp to return false")
	}
}

func TestVerifySlackSignature_WrongSecret(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte("body")
	sig := signRequest("wrong-secret", ts, body)
	if verifySlackSignature("correct-secret", ts, body, sig) {
		t.Error("expected wrong secret to return false")
	}
}

func TestHTTPTransport_OAuthCallback(t *testing.T) {
	tr, _ := newTestTransport(&mockHandler{})

	req := httptest.NewRequest(http.MethodGet, "/slack/oauth/callback", nil)
	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OAuth callback received") {
		t.Errorf("body: want OAuth message, got %q", rec.Body.String())
	}
}

func TestHTTPTransport_SlashCommand_InvalidSignature(t *testing.T) {
	tr, _ := newTestTransport(&mockHandler{})

	req := httptest.NewRequest(http.MethodPost, "/slack/commands", strings.NewReader("command=/sherlock"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Slack-Signature", "v0=badsig")

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: want %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHTTPTransport_Interaction_InvalidSignature(t *testing.T) {
	tr, _ := newTestTransport(&mockHandler{})

	req := httptest.NewRequest(http.MethodPost, "/slack/interactions", strings.NewReader("payload={}"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Slack-Signature", "v0=badsig")

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: want %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHTTPTransport_SlashCommand_ValidRequest(t *testing.T) {
	h := &mockHandler{}
	tr, secret := newTestTransport(h)

	body := "command=%2Fsherlock&text=test-service"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signRequest(secret, timestamp, []byte(body))

	req := httptest.NewRequest(http.MethodPost, "/slack/commands", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d", http.StatusOK, rec.Code)
	}

	time.Sleep(100 * time.Millisecond)
	if h.slashCmd == nil {
		t.Error("expected handler to receive SlashCommand")
	}
}

func TestHTTPTransport_Interaction_BlockActions(t *testing.T) {
	h := &mockHandler{}
	tr, secret := newTestTransport(h)

	payload := `{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"123.456","text":"hello"},"actions":[{"action_id":"approve","value":"yes"}]}`
	formData := url.Values{"payload": {payload}}
	body := formData.Encode()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signRequest(secret, timestamp, []byte(body))

	req := httptest.NewRequest(http.MethodPost, "/slack/interactions?"+body, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHTTPTransport_Interaction_MessageAction(t *testing.T) {
	h := &mockHandler{}
	tr, secret := newTestTransport(h)

	payload := `{"type":"message_action","callback_id":"investigate","user":{"id":"U2"},"channel":{"id":"C2"},"message":{"ts":"789.012","text":"alert: high latency"},"trigger_id":"trig1","response_url":"https://hooks.slack.com/resp"}`
	formData := url.Values{"payload": {payload}}
	body := formData.Encode()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signRequest(secret, timestamp, []byte(body))

	req := httptest.NewRequest(http.MethodPost, "/slack/interactions?"+body, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d", http.StatusOK, rec.Code)
	}

	time.Sleep(100 * time.Millisecond)
	if h.shortcut == nil {
		t.Error("expected handler to receive MessageShortcut for message_action")
	}
}

func TestHTTPTransport_Interaction_UnknownType(t *testing.T) {
	h := &mockHandler{}
	tr, secret := newTestTransport(h)

	payload := `{"type":"view_submission","user":{"id":"U3"},"channel":{"id":"C3"}}`
	formData := url.Values{"payload": {payload}}
	body := formData.Encode()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signRequest(secret, timestamp, []byte(body))

	req := httptest.NewRequest(http.MethodPost, "/slack/interactions?"+body, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHTTPTransport_Interaction_MissingPayload(t *testing.T) {
	tr, secret := newTestTransport(&mockHandler{})

	body := ""
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signRequest(secret, timestamp, []byte(body))

	req := httptest.NewRequest(http.MethodPost, "/slack/interactions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want %d for missing payload, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHTTPTransport_Interaction_InvalidPayloadJSON(t *testing.T) {
	tr, secret := newTestTransport(&mockHandler{})

	formData := url.Values{"payload": {"not-valid-json{"}}
	body := formData.Encode()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signRequest(secret, timestamp, []byte(body))

	req := httptest.NewRequest(http.MethodPost, "/slack/interactions?"+body, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rec := httptest.NewRecorder()
	tr.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want %d for invalid payload JSON, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestNewHTTPTransport_CreatesRouter(t *testing.T) {
	h := &mockHandler{}
	tr := NewHTTPTransport(":3001", "secret", h, zap.NewNop())
	if tr.router == nil {
		t.Fatal("expected non-nil router")
	}
	if tr.signingSecret != "secret" {
		t.Errorf("signingSecret = %q, want %q", tr.signingSecret, "secret")
	}
	if tr.server == nil {
		t.Fatal("expected non-nil server")
	}
}

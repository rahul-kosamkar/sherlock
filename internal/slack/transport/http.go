package transport

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type HTTPTransport struct {
	router        chi.Router
	handler       Handler
	signingSecret string
	logger        *zap.Logger
	server        *http.Server
}

func NewHTTPTransport(address string, signingSecret string, handler Handler, logger *zap.Logger) *HTTPTransport {
	t := &HTTPTransport{
		router:        chi.NewRouter(),
		handler:       handler,
		signingSecret: signingSecret,
		logger:        logger,
	}

	t.router.Post("/slack/commands", t.handleSlashCommand)
	t.router.Post("/slack/interactions", t.handleInteraction)
	t.router.Post("/slack/events", t.handleEvent)
	t.router.Get("/slack/oauth/callback", t.handleOAuthCallback)

	t.server = &http.Server{
		Addr:              address,
		Handler:           t.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return t
}

func (t *HTTPTransport) Start(ctx context.Context) error {
	t.logger.Info("starting HTTP transport", zap.String("address", t.server.Addr))
	errCh := make(chan error, 1)
	go func() {
		if err := t.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http transport failed to start: %w", err)
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}

func (t *HTTPTransport) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return t.server.Shutdown(ctx)
}

func (t *HTTPTransport) handleSlashCommand(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.logger.Error("failed to read request body", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if !t.verifyRequest(r, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		t.logger.Error("failed to parse form", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	cmd := SlashCommand{
		Command:     r.FormValue("command"),
		Text:        r.FormValue("text"),
		UserID:      r.FormValue("user_id"),
		ChannelID:   r.FormValue("channel_id"),
		TeamID:      r.FormValue("team_id"),
		ResponseURL: r.FormValue("response_url"),
		TriggerID:   r.FormValue("trigger_id"),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"response_type": "in_channel",
		"text":          "Acknowledged. Sherlock is on the case.",
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := t.handler.HandleSlashCommand(ctx, cmd); err != nil {
			t.logger.Error("slash command handler failed", zap.Error(err))
		}
	}()
}

func (t *HTTPTransport) handleInteraction(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.logger.Error("failed to read request body", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if !t.verifyRequest(r, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		t.logger.Error("failed to parse form", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	payloadStr := r.FormValue("payload")
	if payloadStr == "" {
		http.Error(w, "missing payload", http.StatusBadRequest)
		return
	}

	var payload struct {
		Type        string `json:"type"`
		CallbackID  string `json:"callback_id"`
		TriggerID   string `json:"trigger_id"`
		ResponseURL string `json:"response_url"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
		Message struct {
			TS   string `json:"ts"`
			Text string `json:"text"`
		} `json:"message"`
		Actions []struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		} `json:"actions"`
	}

	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		t.logger.Error("failed to unmarshal interaction payload", zap.Error(err))
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		switch payload.Type {
		case "message_action":
			shortcut := MessageShortcut{
				CallbackID:  payload.CallbackID,
				UserID:      payload.User.ID,
				ChannelID:   payload.Channel.ID,
				MessageTS:   payload.Message.TS,
				MessageText: payload.Message.Text,
				TriggerID:   payload.TriggerID,
				ResponseURL: payload.ResponseURL,
			}
			if err := t.handler.HandleMessageShortcut(ctx, shortcut); err != nil {
				t.logger.Error("message shortcut handler failed", zap.Error(err))
			}

		case "block_actions":
			for _, a := range payload.Actions {
				action := InteractionAction{
					ActionID:    a.ActionID,
					Value:       a.Value,
					UserID:      payload.User.ID,
					ChannelID:   payload.Channel.ID,
					MessageTS:   payload.Message.TS,
					TriggerID:   payload.TriggerID,
					ResponseURL: payload.ResponseURL,
				}
				if err := t.handler.HandleInteraction(ctx, action); err != nil {
					t.logger.Error("interaction handler failed",
						zap.String("action_id", a.ActionID),
						zap.Error(err),
					)
				}
			}

		default:
			t.logger.Warn("unhandled interaction type", zap.String("type", payload.Type))
		}
	}()
}

func (t *HTTPTransport) handleEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.logger.Error("failed to read event body", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if !t.verifyRequest(r, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var envelope struct {
		Type      string `json:"type"`
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
		Event     struct {
			Type    string `json:"type"`
			User    string `json:"user"`
			Text    string `json:"text"`
			Channel string `json:"channel"`
		} `json:"event"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		t.logger.Error("failed to unmarshal event", zap.Error(err))
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	if envelope.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": envelope.Challenge})
		return
	}

	w.WriteHeader(http.StatusOK)

	if envelope.Type == "event_callback" && envelope.Event.Type == "app_mention" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := t.handler.HandleAppMention(ctx, envelope.Event.Channel, envelope.Event.User, envelope.Event.Text); err != nil {
				t.logger.Error("app mention handler failed", zap.Error(err))
			}
		}()
	}
}

func (t *HTTPTransport) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OAuth callback received. Not yet implemented.")
}

func (t *HTTPTransport) verifyRequest(r *http.Request, body []byte) bool {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	signature := r.Header.Get("X-Slack-Signature")
	return verifySlackSignature(t.signingSecret, timestamp, body, signature)
}

func verifySlackSignature(signingSecret string, timestamp string, body []byte, signature string) bool {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return false
	}

	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

package transport

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"go.uber.org/zap"
)

type SocketTransport struct {
	client   *slack.Client
	socket   *socketmode.Client
	handler  Handler
	logger   *zap.Logger
	cancelFn context.CancelFunc
}

func NewSocketTransport(appToken string, botToken string, handler Handler, logger *zap.Logger) *SocketTransport {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)

	return &SocketTransport{
		client:  api,
		socket:  sm,
		handler: handler,
		logger:  logger,
	}
}

func (t *SocketTransport) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	t.cancelFn = cancel

	go t.listenEvents(ctx)

	t.logger.Info("starting socket mode transport")
	errCh := make(chan error, 1)
	go func() {
		errCh <- t.socket.RunContext(ctx)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("socket mode failed: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *SocketTransport) Stop() error {
	if t.cancelFn != nil {
		t.cancelFn()
	}
	return nil
}

func (t *SocketTransport) listenEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-t.socket.Events:
			if !ok {
				return
			}
			t.dispatchEvent(ctx, evt)
		}
	}
}

func (t *SocketTransport) dispatchEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		evtAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			t.logger.Error("unexpected events API payload type")
			return
		}
		t.socket.Ack(*evt.Request)

		if evtAPI.Type == slackevents.CallbackEvent {
			switch inner := evtAPI.InnerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:
				if err := t.handler.HandleAppMention(ctx, inner.Channel, inner.User, inner.Text); err != nil {
					t.logger.Error("app mention handler failed", zap.Error(err))
				}
			default:
				t.logger.Debug("ignoring inner event", zap.String("type", evtAPI.InnerEvent.Type))
			}
		}

	case socketmode.EventTypeSlashCommand:
		data, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			t.logger.Error("unexpected slash command payload type")
			return
		}
		t.socket.Ack(*evt.Request)
		cmd := SlashCommand{
			Command:     data.Command,
			Text:        data.Text,
			UserID:      data.UserID,
			ChannelID:   data.ChannelID,
			TeamID:      data.TeamID,
			ResponseURL: data.ResponseURL,
			TriggerID:   data.TriggerID,
		}
		if err := t.handler.HandleSlashCommand(ctx, cmd); err != nil {
			t.logger.Error("slash command handler failed", zap.Error(err))
		}

	case socketmode.EventTypeInteractive:
		cb, ok := evt.Data.(slack.InteractionCallback)
		if !ok {
			t.logger.Error("unexpected interaction payload type")
			return
		}
		t.socket.Ack(*evt.Request)

		switch cb.Type {
		case slack.InteractionTypeMessageAction:
			shortcut := MessageShortcut{
				CallbackID:  cb.CallbackID,
				UserID:      cb.User.ID,
				ChannelID:   cb.Channel.ID,
				MessageTS:   cb.MessageTs,
				MessageText: cb.Message.Text,
				TriggerID:   cb.TriggerID,
				ResponseURL: cb.ResponseURL,
			}
			if err := t.handler.HandleMessageShortcut(ctx, shortcut); err != nil {
				t.logger.Error("message shortcut handler failed", zap.Error(err))
			}

		case slack.InteractionTypeBlockActions:
			for _, a := range cb.ActionCallback.BlockActions {
				action := InteractionAction{
					ActionID:    a.ActionID,
					Value:       a.Value,
					UserID:      cb.User.ID,
					ChannelID:   cb.Channel.ID,
					MessageTS:   cb.MessageTs,
					TriggerID:   cb.TriggerID,
					ResponseURL: cb.ResponseURL,
				}
				if err := t.handler.HandleInteraction(ctx, action); err != nil {
					t.logger.Error("interaction handler failed",
						zap.String("action_id", a.ActionID),
						zap.Error(err),
					)
				}
			}

		default:
			t.logger.Warn("unhandled interaction type", zap.String("type", string(cb.Type)))
		}

	default:
		t.logger.Debug("ignoring socket event", zap.String("type", string(evt.Type)))
	}
}

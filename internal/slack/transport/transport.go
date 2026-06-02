package transport

import "context"

type SlashCommand struct {
	Command     string
	Text        string
	UserID      string
	ChannelID   string
	TeamID      string
	ResponseURL string
	TriggerID   string
}

type MessageShortcut struct {
	CallbackID  string
	UserID      string
	ChannelID   string
	MessageTS   string
	MessageText string
	TriggerID   string
	ResponseURL string
}

type InteractionAction struct {
	ActionID    string
	Value       string
	UserID      string
	ChannelID   string
	MessageTS   string
	TriggerID   string
	ResponseURL string
}

type Handler interface {
	HandleSlashCommand(ctx context.Context, cmd SlashCommand) error
	HandleMessageShortcut(ctx context.Context, shortcut MessageShortcut) error
	HandleInteraction(ctx context.Context, action InteractionAction) error
	HandleAppMention(ctx context.Context, channelID, userID, text string) error
}

type Transport interface {
	Start(ctx context.Context) error
	Stop() error
}

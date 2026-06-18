package contracts

import "time"

// Installation tracks a Slack workspace installation with
// its credentials and configuration.
type Installation struct {
	ID            string            `json:"id"`
	TenantID      string            `json:"tenant_id"`
	WorkspaceID   string            `json:"workspace_id"`
	WorkspaceName string            `json:"workspace_name"`
	BotToken      string            `json:"-"` // never serialise tokens
	RefreshToken  string            `json:"-"`
	TokenExpiry   *time.Time        `json:"token_expiry,omitempty"`
	Scopes        []string          `json:"scopes"`
	InstalledAt   time.Time         `json:"installed_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Settings      map[string]string `json:"settings,omitempty"`
}

package contracts

import "time"

// AuditEntry records every significant action for compliance
// and debugging purposes.
type AuditEntry struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	Actor     string            `json:"actor"`
	Action    string            `json:"action"`
	Target    string            `json:"target"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

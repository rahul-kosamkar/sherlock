package contracts

import (
	"context"
	"time"
)

// ActionRequest describes a remediation action to be executed.
type ActionRequest struct {
	ID              string            `json:"id"`
	InvestigationID string            `json:"investigation_id"`
	ActionRef       string            `json:"action_ref"`
	Target          TargetRef         `json:"target"`
	Parameters      map[string]string `json:"parameters,omitempty"`
	RequestedBy     string            `json:"requested_by"`
	RequestedAt     time.Time         `json:"requested_at"`
}

// ActionPlan is the output of a dry-run before execution.
type ActionPlan struct {
	RequestID   string   `json:"request_id"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	Reversible  bool     `json:"reversible"`
	Risk        string   `json:"risk"`
}

// ActionResult records what happened when an action was executed.
type ActionResult struct {
	RequestID  string    `json:"request_id"`
	Success    bool      `json:"success"`
	Output     string    `json:"output"`
	ExecutedAt time.Time `json:"executed_at"`
	ExecutedBy string    `json:"executed_by"`
	ApprovedBy string    `json:"approved_by"`
}

// Remediator executes remediation actions with dry-run support.
type Remediator interface {
	Name() string
	DryRun(ctx context.Context, req ActionRequest) (ActionPlan, error)
	Execute(ctx context.Context, req ActionRequest) (ActionResult, error)
}

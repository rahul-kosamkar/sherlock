package contracts

import (
	"context"
	"time"
)

type EvidenceKind string

const (
	EvidenceLog       EvidenceKind = "log"
	EvidenceMetric    EvidenceKind = "metric"
	EvidenceEvent     EvidenceKind = "event"
	EvidenceDeploy    EvidenceKind = "deploy"
	EvidenceGitChange EvidenceKind = "git_change"
	EvidenceTrace     EvidenceKind = "trace"
	EvidenceConfig    EvidenceKind = "config"
	EvidenceK8sState  EvidenceKind = "k8s_state"
)

type RedactionState string

const (
	RedactionNone    RedactionState = "none"
	RedactionPending RedactionState = "pending"
	RedactionDone    RedactionState = "redacted"
)

// Evidence represents a single piece of collected evidence with
// full provenance for auditability.
type Evidence struct {
	ID             string            `json:"id"`
	InvestigationID string           `json:"investigation_id"`
	Kind           EvidenceKind      `json:"kind"`
	Source         string            `json:"source"`
	Target         TargetRef         `json:"target"`
	CollectedAt    time.Time         `json:"collected_at"`
	ObservedAtFrom time.Time         `json:"observed_at_from"`
	ObservedAtTo   time.Time         `json:"observed_at_to"`
	Summary        string            `json:"summary"`
	BodyRef        string            `json:"body_ref"`
	Query          string            `json:"query"`
	Score          float64           `json:"score"`
	Attributes     map[string]string `json:"attributes"`
	RedactionState RedactionState    `json:"redaction_state"`
}

// CollectRequest is handed to each collector so it knows what to look for.
type CollectRequest struct {
	InvestigationID string      `json:"investigation_id"`
	Alert           NormalizedAlert `json:"alert"`
	Targets         []TargetRef `json:"targets"`
	TimeFrom        time.Time   `json:"time_from"`
	TimeTo          time.Time   `json:"time_to"`
}

// Collector gathers evidence from a specific backend system.
type Collector interface {
	Name() string
	Collect(ctx context.Context, req CollectRequest) ([]Evidence, error)
}

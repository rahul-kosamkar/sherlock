package contracts

import (
	"context"
	"time"
)

type InvestigationStatus string

const (
	StatusPending    InvestigationStatus = "pending"
	StatusCollecting InvestigationStatus = "collecting"
	StatusAnalyzing  InvestigationStatus = "analyzing"
	StatusPublishing InvestigationStatus = "publishing"
	StatusDone       InvestigationStatus = "done"
	StatusFailed     InvestigationStatus = "failed"
)

// Investigation is the top-level work item that tracks the
// lifecycle of a single incident investigation.
type Investigation struct {
	ID          string              `json:"id"`
	TenantID    string              `json:"tenant_id"`
	Status      InvestigationStatus `json:"status"`
	AlertIDs    []string            `json:"alert_ids"`
	Targets     []TargetRef         `json:"targets"`
	TimeFrom    time.Time           `json:"time_from"`
	TimeTo      time.Time           `json:"time_to"`
	Headline    string              `json:"headline"`
	Confidence  float64             `json:"confidence"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	CompletedAt *time.Time          `json:"completed_at,omitempty"`

	// Slack context for publishing results
	SlackChannelID string `json:"slack_channel_id,omitempty"`
	SlackThreadTS  string `json:"slack_thread_ts,omitempty"`

	// Results populated after analysis
	Hypotheses []Hypothesis    `json:"hypotheses,omitempty"`
	Timeline   []TimelineEvent `json:"timeline,omitempty"`
}

// InvestigationResult is the final output of an investigation,
// ready for publishing to Slack or the API.
type InvestigationResult struct {
	InvestigationID    string              `json:"investigation_id"`
	Status             InvestigationStatus `json:"status"`
	Headline           string              `json:"headline"`
	Confidence         float64             `json:"confidence"`
	TopHypotheses      []Hypothesis        `json:"top_hypotheses"`
	TimelineEventIDs   []string            `json:"timeline_event_ids"`
	RecommendedActions []SuggestedFix      `json:"recommended_actions"`

	RCAEngine  string `json:"rca_engine,omitempty"`
	AIProvider string `json:"ai_provider,omitempty"`
	AIModel    string `json:"ai_model,omitempty"`
	PassCount  int    `json:"pass_count,omitempty"`
	RootCause  string `json:"root_cause,omitempty"`
	Severity   string `json:"severity,omitempty"`
	BugFixable bool   `json:"bug_fixable,omitempty"`
}

// InvestigationData bundles all collected evidence for analysis.
type InvestigationData struct {
	Investigation Investigation     `json:"investigation"`
	Alerts        []NormalizedAlert `json:"alerts"`
	Evidence      []Evidence        `json:"evidence"`
}

// InvestigationGraph is an enriched view of the investigation
// after correlation, ready for the RCA engine.
type InvestigationGraph struct {
	Data         InvestigationData `json:"data"`
	Correlations []Correlation     `json:"correlations"`
}

type Correlation struct {
	EvidenceA string  `json:"evidence_a"`
	EvidenceB string  `json:"evidence_b"`
	Type      string  `json:"type"` // temporal, label, topology, deploy_proximity
	Strength  float64 `json:"strength"`
}

// Correlator builds an investigation graph from collected evidence.
type Correlator interface {
	Name() string
	Correlate(ctx context.Context, inv InvestigationData) (InvestigationGraph, error)
}

// RCAEngine ranks hypotheses from an investigation graph.
type RCAEngine interface {
	Name() string
	Rank(ctx context.Context, graph InvestigationGraph) ([]Hypothesis, error)
}

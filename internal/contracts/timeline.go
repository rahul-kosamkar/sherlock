package contracts

import "time"

type TimelineEventKind string

const (
	TimelineAlert       TimelineEventKind = "alert"
	TimelineDeploy      TimelineEventKind = "deploy"
	TimelineK8sEvent    TimelineEventKind = "k8s_event"
	TimelineMetricShift TimelineEventKind = "metric_shift"
	TimelineLogPattern  TimelineEventKind = "log_pattern"
	TimelineAction      TimelineEventKind = "action"
	TimelineRecovery    TimelineEventKind = "recovery"
)

// TimelineEvent represents a single ordered event in an incident timeline.
type TimelineEvent struct {
	ID              string            `json:"id"`
	InvestigationID string            `json:"investigation_id"`
	Timestamp       time.Time         `json:"timestamp"`
	Kind            TimelineEventKind `json:"kind"`
	Source          string            `json:"source"`
	Narrative       string            `json:"narrative"`
	EvidenceIDs     []string          `json:"evidence_ids"`
	Attributes      map[string]string `json:"attributes,omitempty"`
}

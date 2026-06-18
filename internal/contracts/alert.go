package contracts

import (
	"context"
	"net/http"
	"time"
)

type AlertStatus string

const (
	AlertStatusFiring   AlertStatus = "firing"
	AlertStatusResolved AlertStatus = "resolved"
)

type AlertSeverity string

const (
	SeverityCritical AlertSeverity = "critical"
	SeverityWarning  AlertSeverity = "warning"
	SeverityInfo     AlertSeverity = "info"
)

// NormalizedAlert is the common alert envelope that all receivers
// produce, regardless of upstream source format.
type NormalizedAlert struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"`
	TenantID    string            `json:"tenant_id"`
	Status      AlertStatus       `json:"status"`
	Severity    AlertSeverity     `json:"severity"`
	Title       string            `json:"title"`
	Summary     string            `json:"summary"`
	Fingerprint string            `json:"fingerprint"`
	GroupKey    string            `json:"group_key"`
	StartsAt    time.Time         `json:"starts_at"`
	EndsAt      *time.Time        `json:"ends_at,omitempty"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	EntityHints []TargetRef       `json:"entity_hints"`
	Links       []Link            `json:"links"`
	RawRef      string            `json:"raw_ref"`
}

type TargetRef struct {
	Kind        string `json:"kind"` // service, repo, k8s.deployment, k8s.pod, host, cloudwatch.alarm
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name"`
	Cluster     string `json:"cluster,omitempty"`
	Environment string `json:"environment,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Region      string `json:"region,omitempty"`
}

type Link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

// AlertReceiver decodes and verifies inbound webhook payloads
// from a specific alerting source.
type AlertReceiver interface {
	Source() string
	Verify(ctx context.Context, headers http.Header, body []byte) error
	Decode(ctx context.Context, headers http.Header, body []byte) ([]NormalizedAlert, error)
}

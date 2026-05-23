package alertmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type payload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []alert           `json:"alerts"`
}

type alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type Receiver struct{}

func New() *Receiver { return &Receiver{} }

func (r *Receiver) Source() string { return "alertmanager" }

func (r *Receiver) Verify(_ context.Context, _ http.Header, _ []byte) error {
	return nil
}

func (r *Receiver) Decode(_ context.Context, _ http.Header, body []byte) ([]contracts.NormalizedAlert, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshalling alertmanager payload: %w", err)
	}

	out := make([]contracts.NormalizedAlert, 0, len(p.Alerts))
	for _, a := range p.Alerts {
		title := a.Annotations["summary"]
		if title == "" {
			title = a.Labels["alertname"]
		}

		severity := mapSeverity(a.Labels["severity"])

		na := contracts.NormalizedAlert{
			ID:          uuid.NewString(),
			Source:      "alertmanager",
			Status:      mapStatus(a.Status),
			Severity:    severity,
			Title:       title,
			Summary:     a.Annotations["description"],
			Fingerprint: a.Fingerprint,
			GroupKey:    p.GroupKey,
			StartsAt:   a.StartsAt,
			Labels:     a.Labels,
			Annotations: a.Annotations,
			EntityHints: extractEntityHints(a.Labels, p.CommonLabels),
			Links:      buildLinks(a),
		}

		if !a.EndsAt.IsZero() && a.EndsAt.Year() > 1 {
			t := a.EndsAt
			na.EndsAt = &t
		}

		out = append(out, na)
	}
	return out, nil
}

func mapStatus(s string) contracts.AlertStatus {
	switch strings.ToLower(s) {
	case "resolved":
		return contracts.AlertStatusResolved
	default:
		return contracts.AlertStatusFiring
	}
}

func mapSeverity(s string) contracts.AlertSeverity {
	switch strings.ToLower(s) {
	case "critical":
		return contracts.SeverityCritical
	case "info":
		return contracts.SeverityInfo
	default:
		return contracts.SeverityWarning
	}
}

func extractEntityHints(labels, commonLabels map[string]string) []contracts.TargetRef {
	merged := make(map[string]string, len(commonLabels)+len(labels))
	for k, v := range commonLabels {
		merged[k] = v
	}
	for k, v := range labels {
		merged[k] = v
	}

	var hints []contracts.TargetRef
	svc := merged["service"]
	if svc == "" {
		svc = merged["job"]
	}
	if svc != "" {
		hints = append(hints, contracts.TargetRef{
			Kind:      "service",
			Name:      svc,
			Namespace: merged["namespace"],
			Cluster:   merged["cluster"],
		})
	}
	return hints
}

func buildLinks(a alert) []contracts.Link {
	var links []contracts.Link
	if u := a.Annotations["dashboard_url"]; u != "" {
		links = append(links, contracts.Link{Rel: "dashboard", Href: u})
	}
	if a.GeneratorURL != "" {
		links = append(links, contracts.Link{Rel: "generator", Href: a.GeneratorURL})
	}
	if u := a.Annotations["runbook_url"]; u != "" {
		links = append(links, contracts.Link{Rel: "runbook", Href: u})
	}
	return links
}

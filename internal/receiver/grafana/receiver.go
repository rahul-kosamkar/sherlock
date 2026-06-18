package grafana

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type payload struct {
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"`
	Alerts            []alert           `json:"alerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Title             string            `json:"title"`
	State             string            `json:"state"`
	Message           string            `json:"message"`
}

type alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
	Values       map[string]any    `json:"values"`
}

type Receiver struct {
	hmacSecret []byte
}

func New(hmacSecret string) *Receiver {
	return &Receiver{hmacSecret: []byte(hmacSecret)}
}

func (r *Receiver) Source() string { return "grafana" }

func (r *Receiver) Verify(_ context.Context, headers http.Header, body []byte) error {
	if len(r.hmacSecret) == 0 {
		return nil
	}

	sig := headers.Get("X-Grafana-Signature")
	if sig == "" {
		return fmt.Errorf("missing X-Grafana-Signature header")
	}

	mac := hmac.New(sha256.New, r.hmacSecret)
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return fmt.Errorf("HMAC signature mismatch")
	}
	return nil
}

func (r *Receiver) Decode(_ context.Context, _ http.Header, body []byte) ([]contracts.NormalizedAlert, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshalling grafana payload: %w", err)
	}

	out := make([]contracts.NormalizedAlert, 0, len(p.Alerts))
	for _, a := range p.Alerts {
		title := p.Title
		if title == "" {
			title = a.Annotations["summary"]
		}

		severity := mapSeverity(a.Labels["severity"])

		na := contracts.NormalizedAlert{
			ID:          uuid.NewString(),
			Source:      "grafana",
			Status:      mapStatus(a.Status),
			Severity:    severity,
			Title:       title,
			Summary:     a.Annotations["description"],
			Fingerprint: a.Fingerprint,
			GroupKey:    p.GroupKey,
			StartsAt:    a.StartsAt,
			Labels:      a.Labels,
			Annotations: a.Annotations,
			EntityHints: extractEntityHints(a.Labels),
			Links:       buildLinks(a),
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

func extractEntityHints(labels map[string]string) []contracts.TargetRef {
	var hints []contracts.TargetRef
	svc := labels["service"]
	if svc == "" {
		svc = labels["job"]
	}
	if svc != "" {
		hints = append(hints, contracts.TargetRef{
			Kind:      "service",
			Name:      svc,
			Namespace: labels["namespace"],
			Cluster:   labels["cluster"],
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

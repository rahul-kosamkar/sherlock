package rca

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func TestDeployProximityRule_Name(t *testing.T) {
	r := &DeployProximityRule{}
	if got := r.Name(); got != "deploy_proximity" {
		t.Errorf("Name() = %q, want %q", got, "deploy_proximity")
	}
}

func TestDeployProximityRule_NoDeployEvidence(t *testing.T) {
	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: time.Now()},
			},
			Evidence: []contracts.Evidence{
				{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error in logs"},
				{ID: "ev-2", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "cpu spike"},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil when no deploy evidence, got %d hypotheses", len(hypotheses))
	}
}

func TestDeployProximityRule_Within30Minutes(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-20 * time.Minute)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "abc123def456789abcdef0123456789abcdef012",
						"environment": "production",
						"creator":     "deployer",
					},
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.CauseCategory != contracts.CauseDeploy {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseDeploy)
	}
	if h.Confidence < 0.79 || h.Confidence > 0.81 {
		t.Errorf("Confidence = %v, want ~0.8 for deployment within 30 minutes", h.Confidence)
	}
}

func TestDeployProximityRule_Within1Hour(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-45 * time.Minute)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "def456",
						"environment": "staging",
						"creator":     "bob",
					},
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Confidence < 0.69 || h.Confidence > 0.71 {
		t.Errorf("Confidence = %v, want ~0.7 for deployment within 1 hour", h.Confidence)
	}
}

func TestDeployProximityRule_BeyondWindow_NoHypothesis(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-3 * time.Hour)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "old123",
						"environment": "production",
						"creator":     "dev",
					},
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil for deployment beyond 2-hour window, got %d hypotheses", len(hypotheses))
	}
}

func TestDeployProximityRule_ConfidenceBoost_LargeDiff(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-20 * time.Minute)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "abc123",
						"environment": "production",
						"creator":     "deployer",
					},
				},
				{
					ID:     "ev-git",
					Kind:   contracts.EvidenceGitChange,
					Source: "deploy",
					Attributes: map[string]string{
						"files_changed": "10",
						"commit_count":  "5",
					},
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	// Base 0.8 + 0.1 for large diff (>5 files)
	if h.Confidence < 0.89 || h.Confidence > 0.91 {
		t.Errorf("Confidence = %v, want ~0.9 (base 0.8 + 0.1 large diff boost)", h.Confidence)
	}
}

func TestDeployProximityRule_ConfidenceBoost_ErrorLogs(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-20 * time.Minute)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "abc123",
						"environment": "production",
						"creator":     "deployer",
					},
				},
				{
					ID:             "ev-log",
					Kind:           contracts.EvidenceLog,
					Source:         "loki",
					Score:          0.8,
					ObservedAtFrom: deployTime.Add(5 * time.Minute),
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	// Base 0.8 + 0.05 for error logs temporally close
	if h.Confidence < 0.84 || h.Confidence > 0.86 {
		t.Errorf("Confidence = %v, want ~0.85 (base 0.8 + 0.05 error log boost)", h.Confidence)
	}
}

func TestDeployProximityRule_Narrative_IncludesSHAAndEnvironment(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-15 * time.Minute)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "abc123def456789abcdef",
						"environment": "production",
						"creator":     "alice",
					},
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if !strings.Contains(h.Narrative, "abc123de") {
		t.Errorf("Narrative should contain short SHA, got: %q", h.Narrative)
	}
	if !strings.Contains(h.Narrative, "production") {
		t.Errorf("Narrative should contain environment, got: %q", h.Narrative)
	}
	if !strings.Contains(h.Narrative, "alice") {
		t.Errorf("Narrative should contain creator, got: %q", h.Narrative)
	}
}

func TestDeployProximityRule_SuggestedFixes(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-10 * time.Minute)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "abc123",
						"environment": "production",
						"creator":     "deployer",
					},
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if len(h.SuggestedFixes) != 2 {
		t.Fatalf("expected 2 suggested fixes, got %d", len(h.SuggestedFixes))
	}

	rollback := h.SuggestedFixes[0]
	if !strings.Contains(rollback.Title, "Rollback") {
		t.Errorf("first fix title = %q, want rollback suggestion", rollback.Title)
	}
	if !rollback.SafeByDefault {
		t.Error("rollback fix should be safe by default")
	}

	review := h.SuggestedFixes[1]
	if !strings.Contains(review.Title, "Review") {
		t.Errorf("second fix title = %q, want review suggestion", review.Title)
	}
	if !review.SafeByDefault {
		t.Error("review fix should be safe by default")
	}
}

func TestDeployProximityRule_ConfidenceCappedAt095(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-10 * time.Minute)

	graph := contracts.InvestigationGraph{
		Data: contracts.InvestigationData{
			Alerts: []contracts.NormalizedAlert{
				{StartsAt: now},
			},
			Evidence: []contracts.Evidence{
				{
					ID:             "ev-deploy",
					Kind:           contracts.EvidenceDeploy,
					Source:         "deploy",
					ObservedAtFrom: deployTime,
					Attributes: map[string]string{
						"sha":         "abc123",
						"environment": "production",
						"creator":     "deployer",
					},
				},
				{
					ID:     "ev-git",
					Kind:   contracts.EvidenceGitChange,
					Source: "deploy",
					Attributes: map[string]string{
						"files_changed": "50",
						"commit_count":  "20",
					},
				},
				{
					ID:             "ev-log",
					Kind:           contracts.EvidenceLog,
					Source:         "loki",
					Score:          0.9,
					ObservedAtFrom: deployTime.Add(2 * time.Minute),
				},
				{
					ID:             "ev-metric",
					Kind:           contracts.EvidenceMetric,
					Source:         "prometheus",
					Summary:        "memory spike detected",
					Score:          0.8,
					ObservedAtFrom: deployTime.Add(5 * time.Minute),
				},
			},
		},
	}

	hypotheses := (&DeployProximityRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Confidence > 0.95 {
		t.Errorf("Confidence = %v, should be capped at 0.95", h.Confidence)
	}
}

func TestWithinWindow(t *testing.T) {
	base := time.Now()

	tests := []struct {
		name   string
		t1     time.Time
		t2     time.Time
		window time.Duration
		want   bool
	}{
		{"within window", base, base.Add(30 * time.Minute), time.Hour, true},
		{"at boundary", base, base.Add(time.Hour), time.Hour, true},
		{"outside window", base, base.Add(2 * time.Hour), time.Hour, false},
		{"negative direction", base.Add(time.Hour), base, time.Hour, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withinWindow(tt.t1, tt.t2, tt.window)
			if got != tt.want {
				t.Errorf("withinWindow() = %v, want %v", got, tt.want)
			}
		})
	}
}

package timeline

import (
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func TestBuilder_Build_AlertEvents(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Alerts: []contracts.NormalizedAlert{
			{ID: "a1", StartsAt: now, Title: "CPU high", Source: "prom"},
			{ID: "a2", StartsAt: now.Add(time.Minute), Title: "OOM", Source: "prom"},
		},
	}

	events := New().Build(data)

	alertEvents := filterByKind(events, contracts.TimelineAlert)
	if len(alertEvents) != 2 {
		t.Fatalf("expected 2 alert events, got %d", len(alertEvents))
	}
}

func TestBuilder_Build_K8sEvidence(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceEvent, ObservedAtFrom: now, Source: "k8s", Summary: "pod restarted"},
			{ID: "e2", Kind: contracts.EvidenceK8sState, ObservedAtFrom: now.Add(time.Minute), Source: "k8s", Summary: "crashloop"},
		},
	}

	events := New().Build(data)

	k8s := filterByKind(events, contracts.TimelineK8sEvent)
	if len(k8s) != 2 {
		t.Fatalf("expected 2 k8s events, got %d", len(k8s))
	}
}

func TestBuilder_Build_MetricEvidence_HighScore(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceMetric, Score: 0.8, ObservedAtFrom: now, Source: "datadog"},
		},
	}

	events := New().Build(data)

	metric := filterByKind(events, contracts.TimelineMetricShift)
	if len(metric) != 1 {
		t.Fatalf("expected 1 metric shift event, got %d", len(metric))
	}
}

func TestBuilder_Build_MetricEvidence_LowScore(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceMetric, Score: 0.3, ObservedAtFrom: now, Source: "datadog"},
		},
	}

	events := New().Build(data)

	metric := filterByKind(events, contracts.TimelineMetricShift)
	if len(metric) != 0 {
		t.Errorf("expected low-score metric evidence to be filtered out, got %d events", len(metric))
	}
}

func TestBuilder_Build_LogEvidence_HighScore(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceLog, Score: 0.9, ObservedAtFrom: now, Source: "loki"},
		},
	}

	events := New().Build(data)

	logs := filterByKind(events, contracts.TimelineLogPattern)
	if len(logs) != 1 {
		t.Fatalf("expected 1 log pattern event, got %d", len(logs))
	}
}

func TestBuilder_Build_LogEvidence_LowScore(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceLog, Score: 0.2, ObservedAtFrom: now, Source: "loki"},
		},
	}

	events := New().Build(data)

	logs := filterByKind(events, contracts.TimelineLogPattern)
	if len(logs) != 0 {
		t.Errorf("expected low-score log evidence to be filtered out, got %d events", len(logs))
	}
}

func TestBuilder_Build_DeployEvidence(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceDeploy, ObservedAtFrom: now, Source: "argocd", Summary: "deployed v2"},
		},
	}

	events := New().Build(data)

	deploy := filterByKind(events, contracts.TimelineDeploy)
	if len(deploy) != 1 {
		t.Fatalf("expected 1 deploy event, got %d", len(deploy))
	}
}

func TestBuilder_Build_SortedByTimestamp(t *testing.T) {
	t3 := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)
	t1 := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e3", Kind: contracts.EvidenceDeploy, ObservedAtFrom: t3, Source: "argocd"},
			{ID: "e1", Kind: contracts.EvidenceEvent, ObservedAtFrom: t1, Source: "k8s"},
			{ID: "e2", Kind: contracts.EvidenceK8sState, ObservedAtFrom: t2, Source: "k8s"},
		},
	}

	events := New().Build(data)

	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.Before(events[i-1].Timestamp) {
			t.Errorf("events not sorted: events[%d].Timestamp=%v before events[%d].Timestamp=%v",
				i, events[i].Timestamp, i-1, events[i-1].Timestamp)
		}
	}
}

func TestBuilder_Build_AssignsIDs(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Alerts: []contracts.NormalizedAlert{
			{ID: "a1", StartsAt: now, Title: "CPU high"},
		},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceEvent, ObservedAtFrom: now, Source: "k8s"},
		},
	}

	events := New().Build(data)

	for i, ev := range events {
		if ev.ID == "" {
			t.Errorf("events[%d] has empty ID", i)
		}
	}
}

func TestBuilder_Build_AssignsInvestigationID(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-42"},
		Alerts: []contracts.NormalizedAlert{
			{ID: "a1", StartsAt: now, Title: "CPU high"},
		},
	}

	events := New().Build(data)

	for i, ev := range events {
		if ev.InvestigationID != "inv-42" {
			t.Errorf("events[%d].InvestigationID = %q, want %q", i, ev.InvestigationID, "inv-42")
		}
	}
}

func TestBuilder_Build_EmptyData(t *testing.T) {
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
	}

	events := New().Build(data)

	if len(events) != 0 {
		t.Errorf("expected 0 events for empty data, got %d", len(events))
	}
}

func TestBuilder_Build_PreservesAttributes(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	attrs := map[string]string{"region": "us-east-1", "severity": "critical"}
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceEvent, ObservedAtFrom: now, Source: "k8s", Attributes: attrs},
		},
	}

	events := New().Build(data)

	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	for k, v := range attrs {
		if events[0].Attributes[k] != v {
			t.Errorf("expected Attributes[%q] = %q, got %q", k, v, events[0].Attributes[k])
		}
	}
}

func TestBuilder_Build_PreservesSource(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceDeploy, ObservedAtFrom: now, Source: "argocd"},
		},
	}

	events := New().Build(data)

	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	if events[0].Source != "argocd" {
		t.Errorf("expected Source %q, got %q", "argocd", events[0].Source)
	}
}

func TestBuilder_Build_MixedEvidence(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	data := contracts.InvestigationData{
		Investigation: contracts.Investigation{ID: "inv-1"},
		Evidence: []contracts.Evidence{
			{ID: "e1", Kind: contracts.EvidenceEvent, ObservedAtFrom: now, Source: "k8s"},
			{ID: "e2", Kind: contracts.EvidenceMetric, Score: 0.9, ObservedAtFrom: now.Add(time.Minute), Source: "prom"},
			{ID: "e3", Kind: contracts.EvidenceLog, Score: 0.7, ObservedAtFrom: now.Add(2 * time.Minute), Source: "loki"},
			{ID: "e4", Kind: contracts.EvidenceDeploy, ObservedAtFrom: now.Add(3 * time.Minute), Source: "argocd"},
			{ID: "e5", Kind: contracts.EvidenceK8sState, ObservedAtFrom: now.Add(4 * time.Minute), Source: "k8s"},
		},
	}

	events := New().Build(data)

	expected := map[contracts.TimelineEventKind]bool{
		contracts.TimelineK8sEvent:    false,
		contracts.TimelineMetricShift: false,
		contracts.TimelineLogPattern:  false,
		contracts.TimelineDeploy:      false,
	}
	for _, ev := range events {
		expected[ev.Kind] = true
	}
	for kind, found := range expected {
		if !found {
			t.Errorf("expected event kind %q not found in results", kind)
		}
	}
}

func filterByKind(events []contracts.TimelineEvent, kind contracts.TimelineEventKind) []contracts.TimelineEvent {
	var out []contracts.TimelineEvent
	for _, ev := range events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

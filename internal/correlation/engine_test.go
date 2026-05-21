package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func TestEngine_Name(t *testing.T) {
	e := New()
	if got := e.Name(); got != "default" {
		t.Errorf("Name() = %q, want %q", got, "default")
	}
}

func makeEvidence(id string, from, to time.Time, target contracts.TargetRef) contracts.Evidence {
	return contracts.Evidence{
		ID:             id,
		Kind:           contracts.EvidenceMetric,
		Source:         "test",
		Target:         target,
		ObservedAtFrom: from,
		ObservedAtTo:   to,
	}
}

func TestCorrelate_TemporalCorrelation(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(30*time.Second), contracts.TargetRef{Name: "svc-a", Namespace: "ns-a"})
	b := makeEvidence("b", now.Add(1*time.Minute), now.Add(90*time.Second), contracts.TargetRef{Name: "svc-b", Namespace: "ns-b"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	temporal := filterByType(graph.Correlations, "temporal")
	if len(temporal) != 1 {
		t.Fatalf("expected 1 temporal correlation, got %d", len(temporal))
	}
	if temporal[0].Strength <= 0.5 {
		t.Errorf("expected high strength, got %f", temporal[0].Strength)
	}
}

func TestCorrelate_TemporalCorrelation_TooFarApart(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(30*time.Second), contracts.TargetRef{Name: "svc-a", Namespace: "ns-a"})
	b := makeEvidence("b", now.Add(10*time.Minute), now.Add(11*time.Minute), contracts.TargetRef{Name: "svc-b", Namespace: "ns-b"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	temporal := filterByType(graph.Correlations, "temporal")
	if len(temporal) != 0 {
		t.Errorf("expected 0 temporal correlations for 10min gap, got %d", len(temporal))
	}
}

func TestCorrelate_TemporalCorrelation_Overlapping(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(5*time.Minute), contracts.TargetRef{Name: "svc-a", Namespace: "ns-a"})
	b := makeEvidence("b", now.Add(2*time.Minute), now.Add(7*time.Minute), contracts.TargetRef{Name: "svc-b", Namespace: "ns-b"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	temporal := filterByType(graph.Correlations, "temporal")
	if len(temporal) != 1 {
		t.Fatalf("expected 1 temporal correlation, got %d", len(temporal))
	}
	if temporal[0].Strength != 1.0 {
		t.Errorf("expected strength=1.0 for overlapping evidence, got %f", temporal[0].Strength)
	}
}

func TestCorrelate_LabelCorrelation(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	target := contracts.TargetRef{Kind: "service", Namespace: "prod", Name: "payment"}
	a := makeEvidence("a", now, now.Add(time.Second), target)
	b := makeEvidence("b", now, now.Add(time.Second), target)

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	label := filterByType(graph.Correlations, "label")
	if len(label) != 1 {
		t.Fatalf("expected 1 label correlation, got %d", len(label))
	}
	if label[0].Strength != 0.8 {
		t.Errorf("expected strength=0.8, got %f", label[0].Strength)
	}
}

func TestCorrelate_LabelCorrelation_DifferentNames(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(time.Second), contracts.TargetRef{Namespace: "prod", Name: "svc-a"})
	b := makeEvidence("b", now, now.Add(time.Second), contracts.TargetRef{Namespace: "prod", Name: "svc-b"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	label := filterByType(graph.Correlations, "label")
	if len(label) != 0 {
		t.Errorf("expected 0 label correlations for different names, got %d", len(label))
	}
}

func TestCorrelate_LabelCorrelation_EmptyName(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(time.Second), contracts.TargetRef{Namespace: "prod", Name: ""})
	b := makeEvidence("b", now, now.Add(time.Second), contracts.TargetRef{Namespace: "prod", Name: "svc-b"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	label := filterByType(graph.Correlations, "label")
	if len(label) != 0 {
		t.Errorf("expected 0 label correlations when name is empty, got %d", len(label))
	}
}

func TestCorrelate_TopologyCorrelation(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(time.Second), contracts.TargetRef{Namespace: "ns-a", Name: "svc-a", Cluster: "us-east"})
	b := makeEvidence("b", now, now.Add(time.Second), contracts.TargetRef{Namespace: "ns-b", Name: "svc-b", Cluster: "us-east"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	topo := filterByType(graph.Correlations, "topology")
	if len(topo) != 1 {
		t.Fatalf("expected 1 topology correlation, got %d", len(topo))
	}
	if topo[0].Strength != 0.5 {
		t.Errorf("expected strength=0.5, got %f", topo[0].Strength)
	}
}

func TestCorrelate_TopologyCorrelation_SameNamespace(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(time.Second), contracts.TargetRef{Namespace: "prod", Name: "svc-a"})
	b := makeEvidence("b", now, now.Add(time.Second), contracts.TargetRef{Namespace: "prod", Name: "svc-b"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	topo := filterByType(graph.Correlations, "topology")
	if len(topo) != 1 {
		t.Fatalf("expected 1 topology correlation for same namespace, got %d", len(topo))
	}
}

func TestCorrelate_TopologyCorrelation_SameNameSkipped(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	target := contracts.TargetRef{Namespace: "prod", Name: "payment", Cluster: "us-east"}
	a := makeEvidence("a", now, now.Add(time.Second), target)
	b := makeEvidence("b", now, now.Add(time.Second), target)

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	topo := filterByType(graph.Correlations, "topology")
	if len(topo) != 0 {
		t.Errorf("expected 0 topology correlations when namespace+name match, got %d", len(topo))
	}
}

func TestCorrelate_MultipleCorrelations(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	target := contracts.TargetRef{Namespace: "prod", Name: "payment"}
	a := makeEvidence("a", now, now.Add(30*time.Second), target)
	b := makeEvidence("b", now.Add(1*time.Minute), now.Add(90*time.Second), target)

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	temporal := filterByType(graph.Correlations, "temporal")
	label := filterByType(graph.Correlations, "label")

	if len(temporal) != 1 {
		t.Errorf("expected 1 temporal correlation, got %d", len(temporal))
	}
	if len(label) != 1 {
		t.Errorf("expected 1 label correlation, got %d", len(label))
	}
}

func TestCorrelate_NoEvidence(t *testing.T) {
	data := contracts.InvestigationData{Evidence: nil}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Correlations) != 0 {
		t.Errorf("expected 0 correlations, got %d", len(graph.Correlations))
	}
}

func TestCorrelate_SingleEvidence(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	a := makeEvidence("a", now, now.Add(time.Second), contracts.TargetRef{Name: "svc-a"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Correlations) != 0 {
		t.Errorf("expected 0 correlations for single evidence, got %d", len(graph.Correlations))
	}
}

func TestCorrelate_WeakTemporalFiltered(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	// Gap of ~4.5 min -> strength = 1 - (270/300) = 0.1, which is below minCorrelationStr (0.3)
	a := makeEvidence("a", now, now.Add(time.Second), contracts.TargetRef{Name: "svc-a", Namespace: "ns-a"})
	b := makeEvidence("b", now.Add(4*time.Minute+30*time.Second), now.Add(5*time.Minute), contracts.TargetRef{Name: "svc-b", Namespace: "ns-b"})

	data := contracts.InvestigationData{Evidence: []contracts.Evidence{a, b}}
	graph, err := New().Correlate(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	temporal := filterByType(graph.Correlations, "temporal")
	if len(temporal) != 0 {
		t.Errorf("expected weak temporal correlation to be filtered out, got %d with strength %f",
			len(temporal), temporal[0].Strength)
	}
}

func filterByType(correlations []contracts.Correlation, typ string) []contracts.Correlation {
	var out []contracts.Correlation
	for _, c := range correlations {
		if c.Type == typ {
			out = append(out, c)
		}
	}
	return out
}

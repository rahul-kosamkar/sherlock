package rca

import (
	"math"
	"strings"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// ---------------------------------------------------------------------------
// ScoreConfidence
// ---------------------------------------------------------------------------

func TestScoreConfidence_BaseConfidence(t *testing.T) {
	h := &contracts.Hypothesis{Confidence: 0.7}
	got := ScoreConfidence(h, nil)
	if !approxEqual(got, 0.7) {
		t.Errorf("ScoreConfidence() = %v, want 0.7 (no adjustments with empty evidence)", got)
	}
}

func TestScoreConfidence_SupportingBoost(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-2", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-3", Kind: contracts.EvidenceLog, Source: "loki"},
	}
	h := &contracts.Hypothesis{
		Confidence: 0.5,
		Supporting: []string{"ev-1", "ev-2", "ev-3"},
	}

	got := ScoreConfidence(h, allEvidence)
	// 0.5 + min(3*0.05, 0.15) - 0.05 (single kind) = 0.60
	if !approxEqual(got, 0.60) {
		t.Errorf("ScoreConfidence() = %v, want 0.60", got)
	}
}

func TestScoreConfidence_MultipleKinds(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-2", Kind: contracts.EvidenceMetric, Source: "loki"},
		{ID: "ev-3", Kind: contracts.EvidenceEvent, Source: "loki"},
	}
	h := &contracts.Hypothesis{
		Confidence: 0.5,
		Supporting: []string{"ev-1", "ev-2", "ev-3"},
	}

	got := ScoreConfidence(h, allEvidence)
	// 0.5 + 0.15 (boost capped) + 0.05 (>=3 kinds) = 0.70
	if !approxEqual(got, 0.70) {
		t.Errorf("ScoreConfidence() = %v, want 0.70", got)
	}
}

func TestScoreConfidence_MultipleSources(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-2", Kind: contracts.EvidenceLog, Source: "fluentd"},
	}
	h := &contracts.Hypothesis{
		Confidence: 0.5,
		Supporting: []string{"ev-1", "ev-2"},
	}

	got := ScoreConfidence(h, allEvidence)
	// 0.5 + 0.10 (2*0.05 boost) + 0.05 (>=2 sources) - 0.05 (single kind) = 0.60
	if !approxEqual(got, 0.60) {
		t.Errorf("ScoreConfidence() = %v, want 0.60", got)
	}
}

func TestScoreConfidence_ContradictingPenalty(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-c1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-c2", Kind: contracts.EvidenceMetric, Source: "prometheus"},
	}
	h := &contracts.Hypothesis{
		Confidence:    0.7,
		Contradicting: []string{"ev-c1", "ev-c2"},
	}

	got := ScoreConfidence(h, allEvidence)
	// 0.7 - 0.2 (2 * 0.1) = 0.50
	if !approxEqual(got, 0.5) {
		t.Errorf("ScoreConfidence() = %v, want 0.5", got)
	}
}

func TestScoreConfidence_SingleKindPenalty(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
	}
	h := &contracts.Hypothesis{
		Confidence: 0.5,
		Supporting: []string{"ev-1"},
	}

	got := ScoreConfidence(h, allEvidence)
	// 0.5 + 0.05 (boost) - 0.05 (single kind penalty) = 0.50
	if !approxEqual(got, 0.50) {
		t.Errorf("ScoreConfidence() = %v, want 0.50", got)
	}
}

func TestScoreConfidence_Capped_At_0_95(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-2", Kind: contracts.EvidenceMetric, Source: "prometheus"},
		{ID: "ev-3", Kind: contracts.EvidenceEvent, Source: "kubernetes"},
	}
	h := &contracts.Hypothesis{
		Confidence: 0.9,
		Supporting: []string{"ev-1", "ev-2", "ev-3"},
	}

	got := ScoreConfidence(h, allEvidence)
	// 0.9 + 0.15 (boost) + 0.05 (>=3 kinds) + 0.05 (>=2 sources) = 1.15 -> capped at 0.95
	if !approxEqual(got, 0.95) {
		t.Errorf("ScoreConfidence() = %v, want 0.95 (should be capped)", got)
	}
}

func TestScoreConfidence_Floor_At_0(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-c1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-c2", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-c3", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-c4", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-c5", Kind: contracts.EvidenceLog, Source: "loki"},
	}
	h := &contracts.Hypothesis{
		Confidence:    0.1,
		Contradicting: []string{"ev-c1", "ev-c2", "ev-c3", "ev-c4", "ev-c5"},
	}

	got := ScoreConfidence(h, allEvidence)
	// 0.1 - 0.5 (5 * 0.1) = -0.4 -> floored at 0.0
	if !approxEqual(got, 0.0) {
		t.Errorf("ScoreConfidence() = %v, want 0.0 (should be floored at 0)", got)
	}
}

func TestScoreConfidence_NoEvidence(t *testing.T) {
	h := &contracts.Hypothesis{Confidence: 0.6}
	got := ScoreConfidence(h, nil)
	if !approxEqual(got, 0.6) {
		t.Errorf("ScoreConfidence() = %v, want 0.6", got)
	}
}

// ---------------------------------------------------------------------------
// ConfidenceRationale
// ---------------------------------------------------------------------------

func TestConfidenceRationale_Basic(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-2", Kind: contracts.EvidenceMetric, Source: "prometheus"},
	}
	h := &contracts.Hypothesis{
		Supporting: []string{"ev-1", "ev-2"},
	}

	got := ConfidenceRationale(h, allEvidence)

	if !strings.Contains(got, "2 supporting evidence items") {
		t.Errorf("rationale should mention evidence count, got: %s", got)
	}
	if !strings.Contains(got, "2 sources") {
		t.Errorf("rationale should mention source count, got: %s", got)
	}
}

func TestConfidenceRationale_NoContradicting(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
	}
	h := &contracts.Hypothesis{
		Supporting: []string{"ev-1"},
	}

	got := ConfidenceRationale(h, allEvidence)

	if !strings.Contains(got, "No contradicting evidence found") {
		t.Errorf("rationale should note absence of contradicting evidence, got: %s", got)
	}
}

func TestConfidenceRationale_WithContradicting(t *testing.T) {
	allEvidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki"},
		{ID: "ev-c1", Kind: contracts.EvidenceMetric, Source: "prometheus"},
	}
	h := &contracts.Hypothesis{
		Supporting:    []string{"ev-1"},
		Contradicting: []string{"ev-c1"},
	}

	got := ConfidenceRationale(h, allEvidence)

	if !strings.Contains(got, "contradicting evidence items reduce confidence") {
		t.Errorf("rationale should mention contradicting evidence, got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// lookupEvidence
// ---------------------------------------------------------------------------

func TestLookupEvidence_Found(t *testing.T) {
	all := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog},
		{ID: "ev-2", Kind: contracts.EvidenceMetric},
		{ID: "ev-3", Kind: contracts.EvidenceEvent},
	}

	got := lookupEvidence([]string{"ev-1", "ev-3"}, all)
	if len(got) != 2 {
		t.Fatalf("lookupEvidence returned %d items, want 2", len(got))
	}
	if got[0].ID != "ev-1" || got[1].ID != "ev-3" {
		t.Errorf("lookupEvidence IDs = [%s, %s], want [ev-1, ev-3]", got[0].ID, got[1].ID)
	}
}

func TestLookupEvidence_NotFound(t *testing.T) {
	all := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog},
	}

	got := lookupEvidence([]string{"ev-99", "ev-100"}, all)
	if len(got) != 0 {
		t.Errorf("lookupEvidence should return empty for non-existing IDs, got %d items", len(got))
	}
}

func TestLookupEvidence_Partial(t *testing.T) {
	all := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog},
		{ID: "ev-2", Kind: contracts.EvidenceMetric},
	}

	got := lookupEvidence([]string{"ev-1", "ev-99", "ev-2", "ev-missing"}, all)
	if len(got) != 2 {
		t.Fatalf("lookupEvidence returned %d items, want 2 (only existing)", len(got))
	}
	if got[0].ID != "ev-1" || got[1].ID != "ev-2" {
		t.Errorf("lookupEvidence IDs = [%s, %s], want [ev-1, ev-2]", got[0].ID, got[1].ID)
	}
}

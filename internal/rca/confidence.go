package rca

import (
	"fmt"
	"math"
	"strings"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func ScoreConfidence(h *contracts.Hypothesis, allEvidence []contracts.Evidence) float64 {
	confidence := h.Confidence

	supportingEvidence := lookupEvidence(h.Supporting, allEvidence)
	contradictingEvidence := lookupEvidence(h.Contradicting, allEvidence)

	supportBoost := math.Min(float64(len(supportingEvidence))*0.05, 0.15)
	confidence += supportBoost

	kinds := make(map[contracts.EvidenceKind]struct{})
	sources := make(map[string]struct{})
	for _, e := range supportingEvidence {
		kinds[e.Kind] = struct{}{}
		sources[e.Source] = struct{}{}
	}

	if len(kinds) >= 3 {
		confidence += 0.05
	}

	if len(sources) >= 2 {
		confidence += 0.05
	}

	confidence -= float64(len(contradictingEvidence)) * 0.1

	if len(kinds) <= 1 && len(supportingEvidence) > 0 {
		confidence -= 0.05
	}

	return math.Max(0.0, math.Min(0.95, confidence))
}

func ConfidenceRationale(h *contracts.Hypothesis, allEvidence []contracts.Evidence) string {
	supportingEvidence := lookupEvidence(h.Supporting, allEvidence)
	contradictingEvidence := lookupEvidence(h.Contradicting, allEvidence)

	sources := make(map[string]struct{})
	for _, e := range supportingEvidence {
		sources[e.Source] = struct{}{}
	}

	sourceList := make([]string, 0, len(sources))
	for s := range sources {
		sourceList = append(sourceList, s)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Based on %d supporting evidence items from %d sources (%s).",
		len(supportingEvidence), len(sources), strings.Join(sourceList, ", ")))

	if len(contradictingEvidence) == 0 {
		parts = append(parts, "No contradicting evidence found.")
	} else {
		parts = append(parts, fmt.Sprintf("%d contradicting evidence items reduce confidence.", len(contradictingEvidence)))
	}

	return strings.Join(parts, " ")
}

func lookupEvidence(ids []string, all []contracts.Evidence) []contracts.Evidence {
	index := make(map[string]contracts.Evidence, len(all))
	for _, e := range all {
		index[e.ID] = e
	}

	var result []contracts.Evidence
	for _, id := range ids {
		if e, ok := index[id]; ok {
			result = append(result, e)
		}
	}
	return result
}

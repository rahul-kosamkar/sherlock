package correlation

import (
	"context"
	"math"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

const (
	temporalWindow     = 5 * time.Minute
	minCorrelationStr  = 0.3
	labelStrength      = 0.8
	topologyStrength   = 0.5
)

var _ contracts.Correlator = (*Engine)(nil)

type Engine struct{}

func New() *Engine {
	return &Engine{}
}

func (e *Engine) Name() string {
	return "default"
}

func (e *Engine) Correlate(_ context.Context, data contracts.InvestigationData) (contracts.InvestigationGraph, error) {
	evidence := data.Evidence
	var correlations []contracts.Correlation

	for i := 0; i < len(evidence); i++ {
		for j := i + 1; j < len(evidence); j++ {
			a := evidence[i]
			b := evidence[j]

			if c, ok := temporalCorrelation(a, b); ok {
				correlations = append(correlations, c)
			}
			if c, ok := labelCorrelation(a, b); ok {
				correlations = append(correlations, c)
			}
			if c, ok := topologyCorrelation(a, b); ok {
				correlations = append(correlations, c)
			}
		}
	}

	return contracts.InvestigationGraph{
		Data:         data,
		Correlations: correlations,
	}, nil
}

func temporalCorrelation(a, b contracts.Evidence) (contracts.Correlation, bool) {
	gap := observationGap(a, b)
	if gap > temporalWindow {
		return contracts.Correlation{}, false
	}

	strength := 1.0 - (gap.Seconds() / temporalWindow.Seconds())
	strength = math.Max(strength, 0.0)

	if strength <= minCorrelationStr {
		return contracts.Correlation{}, false
	}

	return contracts.Correlation{
		EvidenceA: a.ID,
		EvidenceB: b.ID,
		Type:      "temporal",
		Strength:  strength,
	}, true
}

func labelCorrelation(a, b contracts.Evidence) (contracts.Correlation, bool) {
	if a.Target.Name == "" || b.Target.Name == "" {
		return contracts.Correlation{}, false
	}
	if a.Target.Namespace == b.Target.Namespace && a.Target.Name == b.Target.Name {
		return contracts.Correlation{
			EvidenceA: a.ID,
			EvidenceB: b.ID,
			Type:      "label",
			Strength:  labelStrength,
		}, true
	}
	return contracts.Correlation{}, false
}

func topologyCorrelation(a, b contracts.Evidence) (contracts.Correlation, bool) {
	if a.Target.Namespace == "" || b.Target.Namespace == "" {
		return contracts.Correlation{}, false
	}
	if a.Target.Namespace == b.Target.Namespace && a.Target.Name == b.Target.Name {
		return contracts.Correlation{}, false
	}
	sameCluster := a.Target.Cluster != "" && a.Target.Cluster == b.Target.Cluster
	sameNamespace := a.Target.Namespace == b.Target.Namespace

	if sameCluster || sameNamespace {
		return contracts.Correlation{
			EvidenceA: a.ID,
			EvidenceB: b.ID,
			Type:      "topology",
			Strength:  topologyStrength,
		}, true
	}
	return contracts.Correlation{}, false
}

func observationGap(a, b contracts.Evidence) time.Duration {
	if !a.ObservedAtTo.Before(b.ObservedAtFrom) && !b.ObservedAtTo.Before(a.ObservedAtFrom) {
		return 0
	}

	gapAB := b.ObservedAtFrom.Sub(a.ObservedAtTo)
	gapBA := a.ObservedAtFrom.Sub(b.ObservedAtTo)

	if gapAB < 0 {
		gapAB = -gapAB
	}
	if gapBA < 0 {
		gapBA = -gapBA
	}

	if gapAB < gapBA {
		return gapAB
	}
	return gapBA
}

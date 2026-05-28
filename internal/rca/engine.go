package rca

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

var _ contracts.RCAEngine = (*Engine)(nil)

type Rule interface {
	Name() string
	Evaluate(ctx context.Context, graph contracts.InvestigationGraph) []contracts.Hypothesis
}

type Engine struct {
	rules []Rule
}

func New() *Engine {
	e := &Engine{}
	e.RegisterRule(&CrashLoopRule{})
	e.RegisterRule(&OOMKilledRule{})
	e.RegisterRule(&HighCPURule{})
	e.RegisterRule(&ErrorSpikeRule{})
	e.RegisterRule(&SchedulingFailureRule{})
	e.RegisterRule(&DeployProximityRule{})
	return e
}

func (e *Engine) Name() string {
	return "rule-based"
}

func (e *Engine) RegisterRule(r Rule) {
	e.rules = append(e.rules, r)
}

func (e *Engine) Rank(ctx context.Context, graph contracts.InvestigationGraph) ([]contracts.Hypothesis, error) {
	var hypotheses []contracts.Hypothesis

	for _, r := range e.rules {
		results := r.Evaluate(ctx, graph)
		hypotheses = append(hypotheses, results...)
	}

	for i := range hypotheses {
		hypotheses[i].Confidence = ScoreConfidence(&hypotheses[i], graph.Data.Evidence)
	}

	sort.Slice(hypotheses, func(i, j int) bool {
		return hypotheses[i].Confidence > hypotheses[j].Confidence
	})

	for i := range hypotheses {
		hypotheses[i].ID = uuid.New().String()
	}

	if len(hypotheses) > 5 {
		hypotheses = hypotheses[:5]
	}

	return hypotheses, nil
}

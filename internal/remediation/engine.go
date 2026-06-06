package remediation

import (
	"fmt"
	"os"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type Engine struct {
	policies []Policy
	logger   *zap.Logger
}

func New(logger *zap.Logger) *Engine {
	return &Engine{
		logger: logger,
	}
}

func (e *Engine) LoadPolicies(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read policies file: %w", err)
	}

	var policies []Policy
	if err := yaml.Unmarshal(data, &policies); err != nil {
		return fmt.Errorf("parse policies YAML: %w", err)
	}

	e.policies = append(e.policies, policies...)
	e.logger.Info("loaded remediation policies",
		zap.String("path", path),
		zap.Int("count", len(policies)),
	)
	return nil
}

func (e *Engine) LoadDefaults() {
	e.policies = append(e.policies, defaultPolicies()...)
	e.logger.Info("loaded default remediation policies", zap.Int("count", len(defaultPolicies())))
}

func (e *Engine) Evaluate(hypotheses []contracts.Hypothesis) []contracts.SuggestedFix {
	seen := make(map[string]struct{})
	var fixes []contracts.SuggestedFix

	for _, h := range hypotheses {
		for _, fix := range h.SuggestedFixes {
			if _, dup := seen[fix.Title]; !dup {
				seen[fix.Title] = struct{}{}
				fixes = append(fixes, fix)
			}
		}
	}

	for _, h := range hypotheses {
		for _, p := range e.policies {
			if !policyMatches(p, h) {
				continue
			}
			for _, a := range p.Actions {
				if _, dup := seen[a.Title]; dup {
					continue
				}
				seen[a.Title] = struct{}{}
				fixes = append(fixes, contracts.SuggestedFix{
					Title:         a.Title,
					Description:   a.Description,
					RunbookURL:    a.RunbookURL,
					SafeByDefault: a.SafeByDefault,
				})
			}
		}
	}

	return fixes
}

func policyMatches(p Policy, h contracts.Hypothesis) bool {
	if p.Match.CauseCategory != "" && string(h.CauseCategory) != p.Match.CauseCategory {
		return false
	}
	if h.Confidence < p.Match.ConfidenceMin {
		return false
	}
	return true
}

func defaultPolicies() []Policy {
	return []Policy{
		{
			Name: "deploy_regression",
			Match: PolicyMatch{
				CauseCategory: string(contracts.CauseDeploy),
				ConfidenceMin: 0.6,
			},
			Actions: []PolicyAction{
				{Title: "Rollback to previous version", Description: "Revert to the last known-good deployment to restore service health.", SafeByDefault: true},
				{Title: "Review deployment diff", Description: "Examine the code changes introduced in the suspected deployment."},
			},
		},
		{
			Name: "oom_capacity",
			Match: PolicyMatch{
				CauseCategory: string(contracts.CauseCapacity),
				ConfidenceMin: 0.5,
			},
			Actions: []PolicyAction{
				{Title: "Increase memory/CPU limits", Description: "Raise resource requests and limits for the affected workload.", SafeByDefault: true},
				{Title: "Profile application for leaks", Description: "Run profiling to identify memory or goroutine leaks."},
			},
		},
		{
			Name: "code_bug",
			Match: PolicyMatch{
				CauseCategory: string(contracts.CauseCode),
				ConfidenceMin: 0.7,
			},
			Actions: []PolicyAction{
				{Title: "Review error patterns", Description: "Analyze recurring error signatures in logs to pinpoint the faulty code path."},
				{Title: "Create bug fix PR", Description: "Open a pull request addressing the identified bug.", SafeByDefault: false},
			},
		},
		{
			Name: "infrastructure",
			Match: PolicyMatch{
				CauseCategory: string(contracts.CauseInfra),
				ConfidenceMin: 0.5,
			},
			Actions: []PolicyAction{
				{Title: "Check node health", Description: "Verify the health of underlying compute nodes (CPU, memory, disk, network).", SafeByDefault: true},
				{Title: "Review resource quotas", Description: "Ensure namespace resource quotas and limit ranges are not blocking pods."},
			},
		},
	}
}

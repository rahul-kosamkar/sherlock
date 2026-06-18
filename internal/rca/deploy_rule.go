package rca

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

// DeployProximityRule evaluates whether a recent deployment is
// temporally correlated with the alert and likely caused the incident.
type DeployProximityRule struct{}

func (r *DeployProximityRule) Name() string { return "deploy_proximity" }

func (r *DeployProximityRule) Evaluate(_ context.Context, graph contracts.InvestigationGraph) []contracts.Hypothesis {
	evidence := graph.Data.Evidence
	deploys := findEvidenceByKind(evidence, contracts.EvidenceDeploy)

	if len(deploys) == 0 {
		return nil
	}

	alertTime := graph.Data.Alerts[0].StartsAt
	if alertTime.IsZero() && len(graph.Data.Alerts) > 0 {
		alertTime = time.Now().UTC()
	}

	var bestDeploy *contracts.Evidence
	var bestConfidence float64

	for i := range deploys {
		dep := &deploys[i]
		conf := r.proximityConfidence(dep.ObservedAtFrom, alertTime)
		if conf > bestConfidence {
			bestConfidence = conf
			bestDeploy = dep
		}
	}

	if bestDeploy == nil || bestConfidence == 0 {
		return nil
	}

	gitChanges := findEvidenceByKind(evidence, contracts.EvidenceGitChange)
	logs := findEvidenceByKind(evidence, contracts.EvidenceLog)
	metrics := findEvidenceByKind(evidence, contracts.EvidenceMetric)

	supporting := make([]string, 0, len(deploys)+len(gitChanges)+len(logs)+len(metrics))
	for _, d := range deploys {
		supporting = append(supporting, d.ID)
	}

	// Corroborating: large diffs
	for _, gc := range gitChanges {
		supporting = append(supporting, gc.ID)
		filesChanged, _ := strconv.Atoi(gc.Attributes["files_changed"])
		if filesChanged > 5 {
			bestConfidence += 0.1
		}
	}

	// Corroborating: error logs temporally close to deploy
	for _, l := range logs {
		if l.Score > 0.5 && withinWindow(l.ObservedAtFrom, bestDeploy.ObservedAtFrom, time.Hour) {
			bestConfidence += 0.05
			supporting = append(supporting, l.ID)
			break
		}
	}

	// Corroborating: metric anomalies after deploy
	for _, m := range metrics {
		if m.Score > 0.5 && (evidenceContains(m, "memory") || evidenceContains(m, "cpu")) {
			if m.ObservedAtFrom.After(bestDeploy.ObservedAtFrom) {
				bestConfidence += 0.05
				supporting = append(supporting, m.ID)
				break
			}
		}
	}

	if bestConfidence > 0.95 {
		bestConfidence = 0.95
	}

	narrative := r.buildNarrative(bestDeploy, gitChanges)

	return []contracts.Hypothesis{
		{
			Title:         "Recent deployment likely caused the incident",
			Narrative:     narrative,
			CauseCategory: contracts.CauseDeploy,
			Confidence:    bestConfidence,
			Supporting:    supporting,
			SuggestedFixes: []contracts.SuggestedFix{
				{
					Title:         "Rollback to previous version",
					Description:   "Revert to the last known-good deployment to restore service health.",
					SafeByDefault: true,
				},
				{
					Title:         "Review deployment diff",
					Description:   "Examine the code changes in the deployment for the root cause of the failure.",
					SafeByDefault: true,
				},
			},
		},
	}
}

func (r *DeployProximityRule) proximityConfidence(deployTime, alertTime time.Time) float64 {
	diff := alertTime.Sub(deployTime)
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff <= 30*time.Minute:
		return 0.8
	case diff <= time.Hour:
		return 0.7
	case diff <= 2*time.Hour:
		return 0.5
	default:
		return 0
	}
}

func (r *DeployProximityRule) buildNarrative(deploy *contracts.Evidence, gitChanges []contracts.Evidence) string {
	var sb strings.Builder

	sha := deploy.Attributes["sha"]
	env := deploy.Attributes["environment"]
	creator := deploy.Attributes["creator"]

	sb.WriteString(fmt.Sprintf("Deployment of %s to %s by %s occurred at %s, ",
		shortSHAForRule(sha), env, creator, deploy.ObservedAtFrom.Format(time.RFC3339)))
	sb.WriteString("within close temporal proximity to the alert onset. ")

	if len(gitChanges) > 0 {
		for _, gc := range gitChanges {
			if fc := gc.Attributes["files_changed"]; fc != "" {
				commits := gc.Attributes["commit_count"]
				sb.WriteString(fmt.Sprintf("The deployment included %s files changed across %s commits. ", fc, commits))
				break
			}
		}
	}

	sb.WriteString("This deployment is the primary suspect for the incident.")
	return sb.String()
}

func withinWindow(t1, t2 time.Time, window time.Duration) bool {
	diff := t1.Sub(t2)
	if diff < 0 {
		diff = -diff
	}
	return diff <= window
}

func shortSHAForRule(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

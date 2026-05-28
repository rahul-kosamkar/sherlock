package rca

import (
	"context"
	"strings"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

// CrashLoopRule detects container crash loop patterns.
type CrashLoopRule struct{}

func (r *CrashLoopRule) Name() string { return "crash_loop" }

func (r *CrashLoopRule) Evaluate(_ context.Context, graph contracts.InvestigationGraph) []contracts.Hypothesis {
	evidence := graph.Data.Evidence
	k8sEvidence := findEvidenceByKind(evidence, contracts.EvidenceK8sState)

	var crashEvidence []contracts.Evidence
	for _, e := range k8sEvidence {
		if evidenceContains(e, "CrashLoopBackOff") || evidenceContains(e, "restart") {
			crashEvidence = append(crashEvidence, e)
		}
	}

	if len(crashEvidence) == 0 {
		return nil
	}

	confidence := 0.75
	category := contracts.CauseCode
	narrative := "Container is in CrashLoopBackOff state, indicating repeated crashes on startup."

	deploys := findEvidenceByKind(evidence, contracts.EvidenceDeploy)
	if len(deploys) > 0 {
		confidence += 0.1
		category = contracts.CauseDeploy
		narrative = "Container crash loop detected following a recent deployment, likely a deploy regression."
	}

	return []contracts.Hypothesis{
		{
			Title:         "Container crash loop detected",
			Narrative:     narrative,
			CauseCategory: category,
			Confidence:    confidence,
			Supporting:    append(evidenceIDs(crashEvidence), evidenceIDs(deploys)...),
			SuggestedFixes: []contracts.SuggestedFix{
				{
					Title:         "Check recent deployments and consider rollback",
					Description:   "Review the last deployment diff and rollback if the crash correlates with the release.",
					SafeByDefault: true,
				},
			},
		},
	}
}

// OOMKilledRule detects out-of-memory kill patterns.
type OOMKilledRule struct{}

func (r *OOMKilledRule) Name() string { return "oom_killed" }

func (r *OOMKilledRule) Evaluate(_ context.Context, graph contracts.InvestigationGraph) []contracts.Hypothesis {
	evidence := graph.Data.Evidence
	k8sEvidence := findEvidenceByKind(evidence, contracts.EvidenceK8sState)

	var oomEvidence []contracts.Evidence
	for _, e := range k8sEvidence {
		if evidenceContains(e, "OOMKilled") {
			oomEvidence = append(oomEvidence, e)
		}
	}

	if len(oomEvidence) == 0 {
		return nil
	}

	confidence := 0.8
	narrative := "Container was killed by the OOM killer due to exceeding memory limits."

	metrics := findEvidenceByKind(evidence, contracts.EvidenceMetric)
	var memMetrics []contracts.Evidence
	for _, m := range metrics {
		if evidenceContains(m, "memory") && m.Score > 0.5 {
			memMetrics = append(memMetrics, m)
		}
	}

	if len(memMetrics) > 0 {
		confidence += 0.1
		narrative += " Memory metrics confirm an upward trend prior to the kill."
	}

	return []contracts.Hypothesis{
		{
			Title:         "Out of memory - container killed by OOM",
			Narrative:     narrative,
			CauseCategory: contracts.CauseCapacity,
			Confidence:    confidence,
			Supporting:    append(evidenceIDs(oomEvidence), evidenceIDs(memMetrics)...),
			SuggestedFixes: []contracts.SuggestedFix{
				{
					Title:         "Increase memory limits or investigate memory leak",
					Description:   "Raise container memory limits or profile the application for memory leaks.",
					SafeByDefault: false,
				},
			},
		},
	}
}

// HighCPURule detects CPU anomaly patterns.
type HighCPURule struct{}

func (r *HighCPURule) Name() string { return "high_cpu" }

func (r *HighCPURule) Evaluate(_ context.Context, graph contracts.InvestigationGraph) []contracts.Hypothesis {
	evidence := graph.Data.Evidence
	metrics := findEvidenceByKind(evidence, contracts.EvidenceMetric)

	var cpuEvidence []contracts.Evidence
	for _, m := range metrics {
		if evidenceContains(m, "cpu") && m.Score > 0.5 {
			cpuEvidence = append(cpuEvidence, m)
		}
	}

	if len(cpuEvidence) == 0 {
		return nil
	}

	deploys := findEvidenceByKind(evidence, contracts.EvidenceDeploy)

	if len(deploys) > 0 {
		return []contracts.Hypothesis{
			{
				Title:         "CPU spike after deployment",
				Narrative:     "CPU utilization spiked following a recent deployment, suggesting a performance regression in the new release.",
				CauseCategory: contracts.CauseDeploy,
				Confidence:    0.7,
				Supporting:    append(evidenceIDs(cpuEvidence), evidenceIDs(deploys)...),
				SuggestedFixes: []contracts.SuggestedFix{
					{
						Title:         "Review recent deploy changes",
						Description:   "Inspect the deployment diff for CPU-intensive code paths or misconfigurations.",
						SafeByDefault: true,
					},
				},
			},
		}
	}

	var trafficMetrics []contracts.Evidence
	for _, m := range metrics {
		if evidenceContains(m, "traffic") || evidenceContains(m, "request") || evidenceContains(m, "qps") {
			trafficMetrics = append(trafficMetrics, m)
		}
	}

	if len(trafficMetrics) > 0 {
		return []contracts.Hypothesis{
			{
				Title:         "CPU spike due to increased traffic",
				Narrative:     "CPU utilization increased alongside a rise in traffic metrics, indicating load-driven saturation.",
				CauseCategory: contracts.CauseCapacity,
				Confidence:    0.65,
				Supporting:    append(evidenceIDs(cpuEvidence), evidenceIDs(trafficMetrics)...),
				SuggestedFixes: []contracts.SuggestedFix{
					{
						Title:         "Consider scaling up",
						Description:   "Add replicas or increase CPU limits to handle the traffic surge.",
						SafeByDefault: true,
					},
				},
			},
		}
	}

	return []contracts.Hypothesis{
		{
			Title:         "High CPU utilization detected",
			Narrative:     "CPU metrics show anomalous utilization without a clear trigger.",
			CauseCategory: contracts.CauseCapacity,
			Confidence:    0.55,
			Supporting:    evidenceIDs(cpuEvidence),
			SuggestedFixes: []contracts.SuggestedFix{
				{
					Title:         "Consider scaling up",
					Description:   "Review workload and consider horizontal or vertical scaling.",
					SafeByDefault: true,
				},
			},
		},
	}
}

// ErrorSpikeRule detects error rate increases from log evidence.
type ErrorSpikeRule struct{}

func (r *ErrorSpikeRule) Name() string { return "error_spike" }

func (r *ErrorSpikeRule) Evaluate(_ context.Context, graph contracts.InvestigationGraph) []contracts.Hypothesis {
	evidence := graph.Data.Evidence
	logs := findEvidenceByKind(evidence, contracts.EvidenceLog)

	var errorLogs []contracts.Evidence
	for _, l := range logs {
		if l.Score > 0.5 {
			errorLogs = append(errorLogs, l)
		}
	}

	if len(errorLogs) < 3 {
		return nil
	}

	confidence := 0.6
	category := contracts.CauseCode
	narrative := "Multiple error log patterns detected, indicating an elevated error rate."

	deploys := findEvidenceByKind(evidence, contracts.EvidenceDeploy)
	if len(deploys) > 0 {
		confidence = 0.7
		category = contracts.CauseDeploy
		narrative = "Error rate increase correlates with a recent deployment."
	}

	return []contracts.Hypothesis{
		{
			Title:         "Error rate increase detected",
			Narrative:     narrative,
			CauseCategory: category,
			Confidence:    confidence,
			Supporting:    append(evidenceIDs(errorLogs), evidenceIDs(deploys)...),
			SuggestedFixes: []contracts.SuggestedFix{
				{
					Title:         "Check error logs for root cause pattern",
					Description:   "Aggregate error logs to identify the dominant failure mode.",
					SafeByDefault: true,
				},
			},
		},
	}
}

// SchedulingFailureRule detects Kubernetes scheduling or node issues.
type SchedulingFailureRule struct{}

func (r *SchedulingFailureRule) Name() string { return "scheduling_failure" }

func (r *SchedulingFailureRule) Evaluate(_ context.Context, graph contracts.InvestigationGraph) []contracts.Hypothesis {
	evidence := graph.Data.Evidence
	events := findEvidenceByKind(evidence, contracts.EvidenceEvent)
	k8s := findEvidenceByKind(evidence, contracts.EvidenceK8sState)

	var schedEvidence []contracts.Evidence
	for _, e := range append(events, k8s...) {
		if evidenceContains(e, "FailedScheduling") || evidenceContains(e, "Evicted") || evidenceContains(e, "NodeNotReady") {
			schedEvidence = append(schedEvidence, e)
		}
	}

	if len(schedEvidence) == 0 {
		return nil
	}

	return []contracts.Hypothesis{
		{
			Title:         "Kubernetes scheduling or node issues",
			Narrative:     "Scheduling failures or node issues detected, pods may be unable to run.",
			CauseCategory: contracts.CauseInfra,
			Confidence:    0.7,
			Supporting:    evidenceIDs(schedEvidence),
			SuggestedFixes: []contracts.SuggestedFix{
				{
					Title:         "Check node capacity and resource requests",
					Description:   "Verify cluster node health, available resources, and pod resource requests.",
					SafeByDefault: true,
				},
			},
		},
	}
}

func findEvidenceByKind(evidence []contracts.Evidence, kind contracts.EvidenceKind) []contracts.Evidence {
	var result []contracts.Evidence
	for _, e := range evidence {
		if e.Kind == kind {
			result = append(result, e)
		}
	}
	return result
}

func evidenceContains(e contracts.Evidence, term string) bool {
	lower := strings.ToLower(term)
	if strings.Contains(strings.ToLower(e.Summary), lower) {
		return true
	}
	for _, v := range e.Attributes {
		if strings.Contains(strings.ToLower(v), lower) {
			return true
		}
	}
	return false
}

func evidenceIDs(evidence []contracts.Evidence) []string {
	ids := make([]string, len(evidence))
	for i, e := range evidence {
		ids[i] = e.ID
	}
	return ids
}

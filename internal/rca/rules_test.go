package rca

import (
	"context"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func graphWithEvidence(evidence []contracts.Evidence) contracts.InvestigationGraph {
	return contracts.InvestigationGraph{
		Data: contracts.InvestigationData{Evidence: evidence},
	}
}

// ---------------------------------------------------------------------------
// CrashLoopRule
// ---------------------------------------------------------------------------

func TestCrashLoopRule_Name(t *testing.T) {
	r := &CrashLoopRule{}
	if got := r.Name(); got != "crash_loop" {
		t.Errorf("Name() = %q, want %q", got, "crash_loop")
	}
}

func TestCrashLoopRule_Detected(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod in CrashLoopBackOff"},
	})

	hypotheses := (&CrashLoopRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Title != "Container crash loop detected" {
		t.Errorf("Title = %q, want %q", h.Title, "Container crash loop detected")
	}
	if h.CauseCategory != contracts.CauseCode {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseCode)
	}
	if h.Confidence != 0.75 {
		t.Errorf("Confidence = %v, want 0.75", h.Confidence)
	}
	if len(h.Supporting) != 1 {
		t.Errorf("Supporting count = %d, want 1", len(h.Supporting))
	}
}

func TestCrashLoopRule_WithDeploy(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod in CrashLoopBackOff"},
		{ID: "ev-2", Kind: contracts.EvidenceDeploy, Source: "argocd", Summary: "Deployment v1.2.3 rolled out"},
	})

	hypotheses := (&CrashLoopRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.CauseCategory != contracts.CauseDeploy {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseDeploy)
	}
	if h.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", h.Confidence)
	}
	if len(h.Supporting) != 2 {
		t.Errorf("Supporting count = %d, want 2 (crash + deploy)", len(h.Supporting))
	}
}

func TestCrashLoopRule_RestartKeyword(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod restart count: 5"},
	})

	hypotheses := (&CrashLoopRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}
	if hypotheses[0].Title != "Container crash loop detected" {
		t.Errorf("Title = %q, want %q", hypotheses[0].Title, "Container crash loop detected")
	}
}

func TestCrashLoopRule_NoMatch(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod running normally"},
	})

	hypotheses := (&CrashLoopRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil, got %d hypotheses", len(hypotheses))
	}
}

func TestCrashLoopRule_NoK8sEvidence(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "CrashLoopBackOff in logs"},
	})

	hypotheses := (&CrashLoopRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil when only log evidence present, got %d hypotheses", len(hypotheses))
	}
}

// ---------------------------------------------------------------------------
// OOMKilledRule
// ---------------------------------------------------------------------------

func TestOOMKilledRule_Name(t *testing.T) {
	r := &OOMKilledRule{}
	if got := r.Name(); got != "oom_killed" {
		t.Errorf("Name() = %q, want %q", got, "oom_killed")
	}
}

func TestOOMKilledRule_Detected(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Container OOMKilled"},
	})

	hypotheses := (&OOMKilledRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Title != "Out of memory - container killed by OOM" {
		t.Errorf("Title = %q, want %q", h.Title, "Out of memory - container killed by OOM")
	}
	if h.CauseCategory != contracts.CauseCapacity {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseCapacity)
	}
	if h.Confidence != 0.8 {
		t.Errorf("Confidence = %v, want 0.8", h.Confidence)
	}
}

func TestOOMKilledRule_WithMemoryMetrics(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Container OOMKilled"},
		{ID: "ev-2", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "memory utilization spike", Score: 0.9},
	})

	hypotheses := (&OOMKilledRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9 (base 0.8 + 0.1 memory boost)", h.Confidence)
	}
	if len(h.Supporting) != 2 {
		t.Errorf("Supporting count = %d, want 2 (oom + memory metric)", len(h.Supporting))
	}
	if want := "Memory metrics confirm"; !containsSubstring(h.Narrative, want) {
		t.Errorf("Narrative = %q, should contain %q", h.Narrative, want)
	}
}

func TestOOMKilledRule_NoMemoryMetrics(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Container OOMKilled"},
		{ID: "ev-2", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "memory utilization", Score: 0.3},
	})

	hypotheses := (&OOMKilledRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}
	if hypotheses[0].Confidence != 0.8 {
		t.Errorf("Confidence = %v, want 0.8 (memory metric Score too low to boost)", hypotheses[0].Confidence)
	}
}

func TestOOMKilledRule_NoMatch(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod running healthy"},
	})

	hypotheses := (&OOMKilledRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil, got %d hypotheses", len(hypotheses))
	}
}

// ---------------------------------------------------------------------------
// HighCPURule
// ---------------------------------------------------------------------------

func TestHighCPURule_Name(t *testing.T) {
	r := &HighCPURule{}
	if got := r.Name(); got != "high_cpu" {
		t.Errorf("Name() = %q, want %q", got, "high_cpu")
	}
}

func TestHighCPURule_WithDeploy(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "cpu utilization anomaly", Score: 0.8},
		{ID: "ev-2", Kind: contracts.EvidenceDeploy, Source: "argocd", Summary: "Deployment v2.0.0"},
	})

	hypotheses := (&HighCPURule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Title != "CPU spike after deployment" {
		t.Errorf("Title = %q, want %q", h.Title, "CPU spike after deployment")
	}
	if h.CauseCategory != contracts.CauseDeploy {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseDeploy)
	}
	if h.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7", h.Confidence)
	}
}

func TestHighCPURule_WithTraffic(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "cpu utilization anomaly", Score: 0.8},
		{ID: "ev-2", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "traffic rate increase", Score: 0.6},
	})

	hypotheses := (&HighCPURule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Title != "CPU spike due to increased traffic" {
		t.Errorf("Title = %q, want %q", h.Title, "CPU spike due to increased traffic")
	}
	if h.CauseCategory != contracts.CauseCapacity {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseCapacity)
	}
	if h.Confidence != 0.65 {
		t.Errorf("Confidence = %v, want 0.65", h.Confidence)
	}
}

func TestHighCPURule_NoCorrelation(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "cpu utilization anomaly", Score: 0.8},
	})

	hypotheses := (&HighCPURule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Title != "High CPU utilization detected" {
		t.Errorf("Title = %q, want %q", h.Title, "High CPU utilization detected")
	}
	if h.CauseCategory != contracts.CauseCapacity {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseCapacity)
	}
	if h.Confidence != 0.55 {
		t.Errorf("Confidence = %v, want 0.55", h.Confidence)
	}
}

func TestHighCPURule_LowScore(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "cpu utilization", Score: 0.3},
	})

	hypotheses := (&HighCPURule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil for low-score CPU metric, got %d hypotheses", len(hypotheses))
	}
}

func TestHighCPURule_NoMetrics(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "high cpu usage in logs"},
	})

	hypotheses := (&HighCPURule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil when no metric evidence, got %d hypotheses", len(hypotheses))
	}
}

// ---------------------------------------------------------------------------
// ErrorSpikeRule
// ---------------------------------------------------------------------------

func TestErrorSpikeRule_Name(t *testing.T) {
	r := &ErrorSpikeRule{}
	if got := r.Name(); got != "error_spike" {
		t.Errorf("Name() = %q, want %q", got, "error_spike")
	}
}

func TestErrorSpikeRule_Detected(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: nil pointer", Score: 0.8},
		{ID: "ev-2", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: timeout", Score: 0.7},
		{ID: "ev-3", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: connection refused", Score: 0.9},
		{ID: "ev-4", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: internal failure", Score: 0.6},
	})

	hypotheses := (&ErrorSpikeRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.Title != "Error rate increase detected" {
		t.Errorf("Title = %q, want %q", h.Title, "Error rate increase detected")
	}
	if h.CauseCategory != contracts.CauseCode {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseCode)
	}
	if h.Confidence != 0.6 {
		t.Errorf("Confidence = %v, want 0.6", h.Confidence)
	}
}

func TestErrorSpikeRule_WithDeploy(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: nil pointer", Score: 0.8},
		{ID: "ev-2", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: timeout", Score: 0.7},
		{ID: "ev-3", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: failure", Score: 0.9},
		{ID: "ev-4", Kind: contracts.EvidenceDeploy, Source: "argocd", Summary: "Deployment v3.0.0"},
	})

	hypotheses := (&ErrorSpikeRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.CauseCategory != contracts.CauseDeploy {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseDeploy)
	}
	if h.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7 (boosted by deploy correlation)", h.Confidence)
	}
}

func TestErrorSpikeRule_TooFewErrors(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: nil pointer", Score: 0.8},
		{ID: "ev-2", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: timeout", Score: 0.7},
	})

	hypotheses := (&ErrorSpikeRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil with only 2 high-score logs (need >= 3), got %d hypotheses", len(hypotheses))
	}
}

func TestErrorSpikeRule_LowScoreLogs(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: nil pointer", Score: 0.2},
		{ID: "ev-2", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: timeout", Score: 0.3},
		{ID: "ev-3", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: failure", Score: 0.4},
		{ID: "ev-4", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: rejected", Score: 0.1},
	})

	hypotheses := (&ErrorSpikeRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil when all log scores <= 0.5, got %d hypotheses", len(hypotheses))
	}
}

// ---------------------------------------------------------------------------
// SchedulingFailureRule
// ---------------------------------------------------------------------------

func TestSchedulingFailureRule_Name(t *testing.T) {
	r := &SchedulingFailureRule{}
	if got := r.Name(); got != "scheduling_failure" {
		t.Errorf("Name() = %q, want %q", got, "scheduling_failure")
	}
}

func TestSchedulingFailureRule_FailedScheduling(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceEvent, Source: "kubernetes", Summary: "FailedScheduling: insufficient cpu"},
	})

	hypotheses := (&SchedulingFailureRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}

	h := hypotheses[0]
	if h.CauseCategory != contracts.CauseInfra {
		t.Errorf("CauseCategory = %q, want %q", h.CauseCategory, contracts.CauseInfra)
	}
	if h.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7", h.Confidence)
	}
}

func TestSchedulingFailureRule_Evicted(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod Evicted due to node pressure"},
	})

	hypotheses := (&SchedulingFailureRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}
	if hypotheses[0].CauseCategory != contracts.CauseInfra {
		t.Errorf("CauseCategory = %q, want %q", hypotheses[0].CauseCategory, contracts.CauseInfra)
	}
}

func TestSchedulingFailureRule_NodeNotReady(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceEvent, Source: "kubernetes", Summary: "NodeNotReady condition on node-3"},
	})

	hypotheses := (&SchedulingFailureRule{}).Evaluate(context.Background(), graph)
	if len(hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(hypotheses))
	}
	if hypotheses[0].CauseCategory != contracts.CauseInfra {
		t.Errorf("CauseCategory = %q, want %q", hypotheses[0].CauseCategory, contracts.CauseInfra)
	}
}

func TestSchedulingFailureRule_NoMatch(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceEvent, Source: "kubernetes", Summary: "Scheduled successfully on node-1"},
		{ID: "ev-2", Kind: contracts.EvidenceEvent, Source: "kubernetes", Summary: "Pulled image nginx:latest"},
	})

	hypotheses := (&SchedulingFailureRule{}).Evaluate(context.Background(), graph)
	if hypotheses != nil {
		t.Errorf("expected nil for non-scheduling events, got %d hypotheses", len(hypotheses))
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func TestFindEvidenceByKind(t *testing.T) {
	evidence := []contracts.Evidence{
		{ID: "ev-1", Kind: contracts.EvidenceLog},
		{ID: "ev-2", Kind: contracts.EvidenceMetric},
		{ID: "ev-3", Kind: contracts.EvidenceLog},
		{ID: "ev-4", Kind: contracts.EvidenceK8sState},
		{ID: "ev-5", Kind: contracts.EvidenceLog},
	}

	t.Run("filters logs", func(t *testing.T) {
		got := findEvidenceByKind(evidence, contracts.EvidenceLog)
		if len(got) != 3 {
			t.Errorf("findEvidenceByKind(Log) returned %d items, want 3", len(got))
		}
	})

	t.Run("filters metrics", func(t *testing.T) {
		got := findEvidenceByKind(evidence, contracts.EvidenceMetric)
		if len(got) != 1 {
			t.Errorf("findEvidenceByKind(Metric) returned %d items, want 1", len(got))
		}
	})

	t.Run("returns empty for missing kind", func(t *testing.T) {
		got := findEvidenceByKind(evidence, contracts.EvidenceDeploy)
		if len(got) != 0 {
			t.Errorf("findEvidenceByKind(Deploy) returned %d items, want 0", len(got))
		}
	})
}

func TestEvidenceContains_InSummary(t *testing.T) {
	e := contracts.Evidence{Summary: "Container OOMKilled with exit code 137"}
	if !evidenceContains(e, "OOMKilled") {
		t.Error("evidenceContains should find term in Summary")
	}
}

func TestEvidenceContains_InAttributes(t *testing.T) {
	e := contracts.Evidence{
		Summary:    "pod status changed",
		Attributes: map[string]string{"reason": "CrashLoopBackOff", "container": "api"},
	}
	if !evidenceContains(e, "CrashLoopBackOff") {
		t.Error("evidenceContains should find term in Attributes values")
	}
}

func TestEvidenceContains_CaseInsensitive(t *testing.T) {
	e := contracts.Evidence{Summary: "Container OOMKilled"}
	if !evidenceContains(e, "oomkilled") {
		t.Error("evidenceContains should match case-insensitively")
	}
}

func TestEvidenceContains_NotFound(t *testing.T) {
	e := contracts.Evidence{
		Summary:    "Pod running normally",
		Attributes: map[string]string{"status": "healthy"},
	}
	if evidenceContains(e, "CrashLoopBackOff") {
		t.Error("evidenceContains should return false when term is absent")
	}
}

func TestEvidenceIDs(t *testing.T) {
	evidence := []contracts.Evidence{
		{ID: "ev-1"},
		{ID: "ev-2"},
		{ID: "ev-3"},
	}
	got := evidenceIDs(evidence)
	if len(got) != 3 {
		t.Fatalf("evidenceIDs returned %d items, want 3", len(got))
	}
	for i, want := range []string{"ev-1", "ev-2", "ev-3"} {
		if got[i] != want {
			t.Errorf("evidenceIDs[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// Engine integration
// ---------------------------------------------------------------------------

func TestEngine_Name(t *testing.T) {
	e := New()
	if got := e.Name(); got != "rule-based" {
		t.Errorf("Name() = %q, want %q", got, "rule-based")
	}
}

func TestEngine_Rank_MultipleFires(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-crash", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod in CrashLoopBackOff"},
		{ID: "ev-oom", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Container OOMKilled"},
	})

	e := New()
	hypotheses, err := e.Rank(context.Background(), graph)
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}
	if len(hypotheses) != 2 {
		t.Fatalf("expected 2 hypotheses, got %d", len(hypotheses))
	}

	if hypotheses[0].Confidence < hypotheses[1].Confidence {
		t.Errorf("hypotheses not sorted by confidence: first=%v, second=%v",
			hypotheses[0].Confidence, hypotheses[1].Confidence)
	}

	for i, h := range hypotheses {
		if h.ID == "" {
			t.Errorf("hypothesis[%d] should have an assigned ID", i)
		}
	}
}

func TestEngine_Rank_NoEvidence(t *testing.T) {
	graph := graphWithEvidence(nil)

	e := New()
	hypotheses, err := e.Rank(context.Background(), graph)
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}
	if len(hypotheses) != 0 {
		t.Errorf("expected 0 hypotheses for empty evidence, got %d", len(hypotheses))
	}
}

func TestEngine_Rank_MaxFive(t *testing.T) {
	graph := graphWithEvidence([]contracts.Evidence{
		{ID: "ev-crash", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Pod in CrashLoopBackOff"},
		{ID: "ev-oom", Kind: contracts.EvidenceK8sState, Source: "kubernetes", Summary: "Container OOMKilled"},
		{ID: "ev-cpu", Kind: contracts.EvidenceMetric, Source: "prometheus", Summary: "cpu utilization spike", Score: 0.9},
		{ID: "ev-log1", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: nil pointer", Score: 0.8},
		{ID: "ev-log2", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: timeout", Score: 0.7},
		{ID: "ev-log3", Kind: contracts.EvidenceLog, Source: "loki", Summary: "error: connection refused", Score: 0.9},
		{ID: "ev-sched", Kind: contracts.EvidenceEvent, Source: "kubernetes", Summary: "FailedScheduling: insufficient memory"},
	})

	e := New()
	hypotheses, err := e.Rank(context.Background(), graph)
	if err != nil {
		t.Fatalf("Rank() error: %v", err)
	}
	if len(hypotheses) > 5 {
		t.Errorf("expected at most 5 hypotheses, got %d", len(hypotheses))
	}
	if len(hypotheses) != 5 {
		t.Errorf("expected exactly 5 hypotheses (all rules fire), got %d", len(hypotheses))
	}

	for i := 1; i < len(hypotheses); i++ {
		if hypotheses[i].Confidence > hypotheses[i-1].Confidence {
			t.Errorf("hypotheses not sorted: [%d].Confidence=%v > [%d].Confidence=%v",
				i, hypotheses[i].Confidence, i-1, hypotheses[i-1].Confidence)
		}
	}
}

// containsSubstring is a test helper for readable narrative assertions.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsCheck(s, substr))
}

func containsCheck(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

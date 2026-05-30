package llm

import (
	"strings"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func TestParseAnalysis_FullResponse(t *testing.T) {
	raw := `SUMMARY: Pod is crash-looping due to OOM
ROOT_CAUSE: The application heap exceeds the 512Mi memory limit after processing large payloads. Exit code 137 confirms SIGKILL by the OOM killer.
SEVERITY: critical
EXIT_TYPE: oom
ACTION_REQUIRED: yes
BUG_FIXABLE: yes
CONFIDENCE: high
RECOMMENDATIONS:
- Increase memory limit to 1Gi
- Add heap profiling to detect leaks
- Implement payload size validation
FOLLOW_UP:
- TRACE_LOGS: abc123, def456
- POD_EVENTS: all`

	a := ParseAnalysis(raw)

	if a.Summary != "Pod is crash-looping due to OOM" {
		t.Errorf("Summary = %q, want %q", a.Summary, "Pod is crash-looping due to OOM")
	}
	if !strings.Contains(a.RootCause, "512Mi memory limit") {
		t.Errorf("RootCause = %q, want it to contain %q", a.RootCause, "512Mi memory limit")
	}
	if !strings.Contains(a.RootCause, "Exit code 137") {
		t.Errorf("RootCause = %q, want it to contain %q", a.RootCause, "Exit code 137")
	}
	if a.Severity != "critical" {
		t.Errorf("Severity = %q, want %q", a.Severity, "critical")
	}
	if a.ExitType != "oom" {
		t.Errorf("ExitType = %q, want %q", a.ExitType, "oom")
	}
	if !a.ActionRequired {
		t.Error("ActionRequired = false, want true")
	}
	if !a.BugFixable {
		t.Error("BugFixable = false, want true")
	}
	if a.Confidence != "high" {
		t.Errorf("Confidence = %q, want %q", a.Confidence, "high")
	}
	if len(a.Recommendations) != 3 {
		t.Fatalf("len(Recommendations) = %d, want 3", len(a.Recommendations))
	}
	if a.Recommendations[0] != "Increase memory limit to 1Gi" {
		t.Errorf("Recommendations[0] = %q, want %q", a.Recommendations[0], "Increase memory limit to 1Gi")
	}
	if a.Recommendations[1] != "Add heap profiling to detect leaks" {
		t.Errorf("Recommendations[1] = %q, want %q", a.Recommendations[1], "Add heap profiling to detect leaks")
	}
	if a.Recommendations[2] != "Implement payload size validation" {
		t.Errorf("Recommendations[2] = %q, want %q", a.Recommendations[2], "Implement payload size validation")
	}
	if len(a.FollowUps) != 2 {
		t.Fatalf("len(FollowUps) = %d, want 2", len(a.FollowUps))
	}
	if a.RawResponse != raw {
		t.Error("RawResponse does not match input")
	}
}

func TestParseAnalysis_MultilineRootCause(t *testing.T) {
	raw := `SUMMARY: Service degraded
ROOT_CAUSE: First line of root cause.
Second line continues the analysis.
Third line with more detail.
SEVERITY: high`

	a := ParseAnalysis(raw)

	if !strings.Contains(a.RootCause, "First line") {
		t.Errorf("RootCause = %q, want it to contain %q", a.RootCause, "First line")
	}
	if !strings.Contains(a.RootCause, "Second line") {
		t.Errorf("RootCause = %q, want it to contain %q", a.RootCause, "Second line")
	}
	if !strings.Contains(a.RootCause, "Third line") {
		t.Errorf("RootCause = %q, want it to contain %q", a.RootCause, "Third line")
	}
}

func TestParseAnalysis_MinimalResponse(t *testing.T) {
	raw := `SUMMARY: Something broke
ROOT_CAUSE: Unknown at this time`

	a := ParseAnalysis(raw)

	if a.Summary != "Something broke" {
		t.Errorf("Summary = %q, want %q", a.Summary, "Something broke")
	}
	if a.RootCause != "Unknown at this time" {
		t.Errorf("RootCause = %q, want %q", a.RootCause, "Unknown at this time")
	}
	if a.Severity != "" {
		t.Errorf("Severity = %q, want empty", a.Severity)
	}
	if a.ExitType != "" {
		t.Errorf("ExitType = %q, want empty", a.ExitType)
	}
	if a.Confidence != "" {
		t.Errorf("Confidence = %q, want empty", a.Confidence)
	}
	if len(a.Recommendations) != 0 {
		t.Errorf("len(Recommendations) = %d, want 0", len(a.Recommendations))
	}
}

func TestParseAnalysis_ActionRequired_Yes(t *testing.T) {
	raw := `SUMMARY: s
ROOT_CAUSE: r
ACTION_REQUIRED: yes`

	a := ParseAnalysis(raw)
	if !a.ActionRequired {
		t.Error("ActionRequired = false, want true")
	}
}

func TestParseAnalysis_ActionRequired_No(t *testing.T) {
	raw := `SUMMARY: s
ROOT_CAUSE: r
ACTION_REQUIRED: no`

	a := ParseAnalysis(raw)
	if a.ActionRequired {
		t.Error("ActionRequired = true, want false")
	}
}

func TestParseAnalysis_ActionRequired_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"uppercase", "YES", true},
		{"titlecase", "Yes", true},
		{"lowercase", "yes", true},
		{"no", "No", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := "SUMMARY: s\nROOT_CAUSE: r\nACTION_REQUIRED: " + tt.value
			a := ParseAnalysis(raw)
			if a.ActionRequired != tt.want {
				t.Errorf("ActionRequired = %v, want %v", a.ActionRequired, tt.want)
			}
		})
	}
}

func TestParseAnalysis_BugFixable_Yes(t *testing.T) {
	raw := `SUMMARY: s
ROOT_CAUSE: r
BUG_FIXABLE: yes`

	a := ParseAnalysis(raw)
	if !a.BugFixable {
		t.Error("BugFixable = false, want true")
	}
}

func TestParseAnalysis_Confidence_Values(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"high", "high", "high"},
		{"medium", "medium", "medium"},
		{"low", "low", "low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := "SUMMARY: s\nROOT_CAUSE: r\nCONFIDENCE: " + tt.value
			a := ParseAnalysis(raw)
			if a.Confidence != tt.want {
				t.Errorf("Confidence = %q, want %q", a.Confidence, tt.want)
			}
		})
	}
}

func TestParseAnalysis_Recommendations(t *testing.T) {
	raw := `SUMMARY: s
ROOT_CAUSE: r
RECOMMENDATIONS:
- First recommendation
- Second recommendation
- Third recommendation`

	a := ParseAnalysis(raw)
	if len(a.Recommendations) != 3 {
		t.Fatalf("len(Recommendations) = %d, want 3", len(a.Recommendations))
	}
	if a.Recommendations[0] != "First recommendation" {
		t.Errorf("Recommendations[0] = %q, want %q", a.Recommendations[0], "First recommendation")
	}
	if a.Recommendations[1] != "Second recommendation" {
		t.Errorf("Recommendations[1] = %q, want %q", a.Recommendations[1], "Second recommendation")
	}
	if a.Recommendations[2] != "Third recommendation" {
		t.Errorf("Recommendations[2] = %q, want %q", a.Recommendations[2], "Third recommendation")
	}
}

func TestParseAnalysis_EmptyInput(t *testing.T) {
	a := ParseAnalysis("")

	if a.Summary != "" {
		t.Errorf("Summary = %q, want empty", a.Summary)
	}
	if a.RootCause != "" {
		t.Errorf("RootCause = %q, want empty", a.RootCause)
	}
	if a.RawResponse != "" {
		t.Errorf("RawResponse = %q, want empty", a.RawResponse)
	}
}

func TestParseAnalysis_GarbageInput(t *testing.T) {
	raw := "This is just random text without any structured fields at all."
	a := ParseAnalysis(raw)

	if a.RawResponse != raw {
		t.Error("RawResponse should equal the original input")
	}
}

func TestParseFollowUps_AllTools(t *testing.T) {
	raw := `SUMMARY: s
ROOT_CAUSE: r
FOLLOW_UP:
- TRACE_LOGS: abc123
- TIME_WINDOW_LOGS: 2024-01-01T00:00:00Z/2024-01-01T01:00:00Z
- POD_EVENTS: all
- GITHUB_FILES: main.go, handler.go
- LOG_QUERY: {app="myservice"} |= "error"`

	fus := ParseFollowUps(raw)
	if len(fus) != 5 {
		t.Fatalf("len(FollowUps) = %d, want 5", len(fus))
	}

	expected := []struct {
		tool  string
		value string
	}{
		{"TRACE_LOGS", "abc123"},
		{"TIME_WINDOW_LOGS", "2024-01-01T00:00:00Z/2024-01-01T01:00:00Z"},
		{"POD_EVENTS", "all"},
		{"GITHUB_FILES", "main.go, handler.go"},
		{"LOG_QUERY", `{app="myservice"} |= "error"`},
	}
	for i, exp := range expected {
		if fus[i].Tool != exp.tool {
			t.Errorf("FollowUps[%d].Tool = %q, want %q", i, fus[i].Tool, exp.tool)
		}
		if fus[i].Value != exp.value {
			t.Errorf("FollowUps[%d].Value = %q, want %q", i, fus[i].Value, exp.value)
		}
	}
}

func TestParseFollowUps_TraceLogs_MultipleIDs(t *testing.T) {
	raw := `FOLLOW_UP:
- TRACE_LOGS: id1, id2, id3`

	fus := ParseFollowUps(raw)
	if len(fus) != 1 {
		t.Fatalf("len(FollowUps) = %d, want 1", len(fus))
	}
	if fus[0].Value != "id1, id2, id3" {
		t.Errorf("Value = %q, want %q", fus[0].Value, "id1, id2, id3")
	}
}

func TestParseFollowUps_NoFollowUp(t *testing.T) {
	raw := `SUMMARY: s
ROOT_CAUSE: r
RECOMMENDATIONS:
- Do something`

	fus := ParseFollowUps(raw)
	if fus != nil {
		t.Errorf("FollowUps = %v, want nil", fus)
	}
}

func TestParseFollowUps_EmptyFollowUp(t *testing.T) {
	raw := `FOLLOW_UP:
`
	fus := ParseFollowUps(raw)
	if len(fus) != 0 {
		t.Errorf("len(FollowUps) = %d, want 0", len(fus))
	}
}

func TestParseFollowUps_MixedCase(t *testing.T) {
	raw := `FOLLOW_UP:
- trace_logs: abc
- TRACE_LOGS: def`

	fus := ParseFollowUps(raw)
	if len(fus) != 1 {
		t.Fatalf("len(FollowUps) = %d, want 1 (only uppercase matches)", len(fus))
	}
	if fus[0].Tool != "TRACE_LOGS" {
		t.Errorf("Tool = %q, want %q", fus[0].Tool, "TRACE_LOGS")
	}
	if fus[0].Value != "def" {
		t.Errorf("Value = %q, want %q", fus[0].Value, "def")
	}
}

// --- MapToHypotheses tests ---

func makeEvidence(ids ...string) []contracts.Evidence {
	ev := make([]contracts.Evidence, len(ids))
	for i, id := range ids {
		ev[i] = contracts.Evidence{ID: id}
	}
	return ev
}

func TestMapToHypotheses_Basic(t *testing.T) {
	a := &LLMAnalysis{
		Summary:   "Pod crash due to OOM",
		RootCause: "Memory exhaustion",
	}
	hyps := MapToHypotheses(a, nil)

	if len(hyps) != 1 {
		t.Fatalf("len(hyps) = %d, want 1", len(hyps))
	}
	if hyps[0].Title != "Pod crash due to OOM" {
		t.Errorf("Title = %q, want %q", hyps[0].Title, "Pod crash due to OOM")
	}
	if hyps[0].Narrative != "Memory exhaustion" {
		t.Errorf("Narrative = %q, want %q", hyps[0].Narrative, "Memory exhaustion")
	}
}

func TestMapToHypotheses_ConfidenceMapping(t *testing.T) {
	tests := []struct {
		name       string
		confidence string
		want       float64
	}{
		{"high", "high", 0.85},
		{"medium", "medium", 0.65},
		{"low", "low", 0.4},
		{"empty", "", 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &LLMAnalysis{Confidence: tt.confidence}
			hyps := MapToHypotheses(a, nil)
			if hyps[0].Confidence != tt.want {
				t.Errorf("Confidence = %v, want %v", hyps[0].Confidence, tt.want)
			}
		})
	}
}

func TestMapToHypotheses_CauseCategory_OOM(t *testing.T) {
	a := &LLMAnalysis{RootCause: "Container was killed due to oom, heap limit exceeded"}
	hyps := MapToHypotheses(a, nil)
	if hyps[0].CauseCategory != contracts.CauseCapacity {
		t.Errorf("CauseCategory = %q, want %q", hyps[0].CauseCategory, contracts.CauseCapacity)
	}
}

func TestMapToHypotheses_CauseCategory_Deploy(t *testing.T) {
	a := &LLMAnalysis{RootCause: "Recent deployment introduced a regression in the handler"}
	hyps := MapToHypotheses(a, nil)
	if hyps[0].CauseCategory != contracts.CauseDeploy {
		t.Errorf("CauseCategory = %q, want %q", hyps[0].CauseCategory, contracts.CauseDeploy)
	}
}

func TestMapToHypotheses_CauseCategory_Code(t *testing.T) {
	a := &LLMAnalysis{RootCause: "A nil pointer dereference in the request handler causes a panic"}
	hyps := MapToHypotheses(a, nil)
	if hyps[0].CauseCategory != contracts.CauseCode {
		t.Errorf("CauseCategory = %q, want %q", hyps[0].CauseCategory, contracts.CauseCode)
	}
}

func TestMapToHypotheses_CauseCategory_Infra(t *testing.T) {
	a := &LLMAnalysis{RootCause: "Node scheduling failure prevented pod from starting"}
	hyps := MapToHypotheses(a, nil)
	if hyps[0].CauseCategory != contracts.CauseInfra {
		t.Errorf("CauseCategory = %q, want %q", hyps[0].CauseCategory, contracts.CauseInfra)
	}
}

func TestMapToHypotheses_CauseCategory_Config(t *testing.T) {
	a := &LLMAnalysis{RootCause: "A misconfiguration of the database connection string caused failures"}
	hyps := MapToHypotheses(a, nil)
	if hyps[0].CauseCategory != contracts.CauseConfig {
		t.Errorf("CauseCategory = %q, want %q", hyps[0].CauseCategory, contracts.CauseConfig)
	}
}

func TestMapToHypotheses_CauseCategory_Default(t *testing.T) {
	a := &LLMAnalysis{RootCause: "Something happened but no keywords match"}
	hyps := MapToHypotheses(a, nil)
	if hyps[0].CauseCategory != contracts.CauseCode {
		t.Errorf("CauseCategory = %q, want %q (default)", hyps[0].CauseCategory, contracts.CauseCode)
	}
}

func TestMapToHypotheses_Recommendations(t *testing.T) {
	a := &LLMAnalysis{
		Recommendations: []string{"Restart the pod", "Check memory limits", "Review recent commits"},
	}
	hyps := MapToHypotheses(a, nil)

	if len(hyps[0].SuggestedFixes) != 3 {
		t.Fatalf("len(SuggestedFixes) = %d, want 3", len(hyps[0].SuggestedFixes))
	}
	for i, rec := range a.Recommendations {
		fix := hyps[0].SuggestedFixes[i]
		if fix.Description != rec {
			t.Errorf("SuggestedFixes[%d].Description = %q, want %q", i, fix.Description, rec)
		}
		wantTitle := strings.Replace("Step N", "N", strings.TrimSpace(strings.Split(fix.Title, " ")[1]), 1)
		_ = wantTitle
		if fix.Title == "" {
			t.Errorf("SuggestedFixes[%d].Title is empty", i)
		}
	}
}

func TestMapToHypotheses_EmptyAnalysis(t *testing.T) {
	a := &LLMAnalysis{}
	hyps := MapToHypotheses(a, nil)

	if len(hyps) != 1 {
		t.Fatalf("len(hyps) = %d, want 1", len(hyps))
	}
	if hyps[0].Title != "" {
		t.Errorf("Title = %q, want empty", hyps[0].Title)
	}
	if hyps[0].Narrative != "" {
		t.Errorf("Narrative = %q, want empty", hyps[0].Narrative)
	}
	if hyps[0].Confidence != 0.5 {
		t.Errorf("Confidence = %v, want 0.5 (default)", hyps[0].Confidence)
	}
	if len(hyps[0].SuggestedFixes) != 0 {
		t.Errorf("len(SuggestedFixes) = %d, want 0", len(hyps[0].SuggestedFixes))
	}
}

func TestMapToHypotheses_SupportingEvidence(t *testing.T) {
	evidence := makeEvidence("ev-1", "ev-2", "ev-3")
	a := &LLMAnalysis{Summary: "test"}
	hyps := MapToHypotheses(a, evidence)

	if len(hyps[0].Supporting) != 3 {
		t.Fatalf("len(Supporting) = %d, want 3", len(hyps[0].Supporting))
	}
	for i, id := range []string{"ev-1", "ev-2", "ev-3"} {
		if hyps[0].Supporting[i] != id {
			t.Errorf("Supporting[%d] = %q, want %q", i, hyps[0].Supporting[i], id)
		}
	}
}

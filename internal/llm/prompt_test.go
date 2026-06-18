package llm

import (
	"strings"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func newTestAlert() contracts.NormalizedAlert {
	return contracts.NormalizedAlert{
		ID:       "alert-001",
		Source:   "prometheus",
		TenantID: "tenant-1",
		Status:   contracts.AlertStatusFiring,
		Severity: contracts.SeverityCritical,
		Title:    "HighMemoryUsage",
		Summary:  "Pod payment-svc-abc is using 98% memory",
		StartsAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Labels: map[string]string{
			"service":   "payment-svc",
			"namespace": "production",
			"cluster":   "us-east-1",
		},
		Annotations: map[string]string{
			"runbook": "https://runbooks.example.com/memory",
		},
		EntityHints: []contracts.TargetRef{
			{Kind: "k8s.pod", Namespace: "production", Name: "payment-svc-abc", Cluster: "us-east-1"},
		},
	}
}

func newTestBundle() EvidenceBundle {
	return EvidenceBundle{
		Alert:      newTestAlert(),
		SourceCode: make(map[string]string),
	}
}

// --- BuildPass1Prompt tests ---

func TestBuildPass1Prompt_ContainsAlertInfo(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildPass1Prompt(bundle)

	for _, want := range []string{
		"HighMemoryUsage",
		"prometheus",
		"critical",
		"Pod payment-svc-abc is using 98% memory",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildPass1Prompt_ContainsWorkload(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	bundle.Alert.Labels["service"] = "payment-svc"

	prompt := BuildPass1Prompt(bundle)
	if !strings.Contains(prompt, "payment-svc") {
		t.Error("prompt should contain 'payment-svc'")
	}
}

func TestBuildPass1Prompt_ContainsWorkload_AppLabel(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	delete(bundle.Alert.Labels, "service")
	bundle.Alert.Labels["app"] = "order-api"

	prompt := BuildPass1Prompt(bundle)
	if !strings.Contains(prompt, "order-api") {
		t.Error("prompt should contain 'order-api' when 'service' label is absent")
	}
}

func TestBuildPass1Prompt_ContainsGuidelines(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildPass1Prompt(bundle)

	for _, want := range []string{
		"Exit code 137",
		"OOM",
		"istio-proxy",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("guidelines missing %q", want)
		}
	}
}

func TestBuildPass1Prompt_ContainsEvidenceSections(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	bundle.PodStatus = "Running: 1/1"
	bundle.ErrorLogs = "ERROR: connection refused"
	bundle.MetricSummary = "memory_usage=98%"

	prompt := BuildPass1Prompt(bundle)
	for _, want := range []string{
		"Pod Status",
		"Error Logs",
		"Resource Metrics",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing evidence section %q", want)
		}
	}
}

func TestBuildPass1Prompt_OmitsEmptySections(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	bundle.PodEvents = ""
	bundle.DeploymentInfo = ""

	prompt := BuildPass1Prompt(bundle)
	if strings.Contains(prompt, "--- Pod Events ---") {
		t.Error("prompt should omit empty Pod Events section")
	}
	if strings.Contains(prompt, "--- Deployment Info ---") {
		t.Error("prompt should omit empty Deployment Info section")
	}
}

func TestBuildPass1Prompt_ContainsFollowUpTools(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildPass1Prompt(bundle)

	tools := []string{"TRACE_LOGS", "TIME_WINDOW_LOGS", "POD_EVENTS", "GITHUB_FILES", "LOG_QUERY"}
	for _, tool := range tools {
		if !strings.Contains(prompt, tool) {
			t.Errorf("prompt missing follow-up tool %q", tool)
		}
	}
}

func TestBuildPass1Prompt_ContainsOutputFormat(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildPass1Prompt(bundle)

	fields := []string{
		"SUMMARY:",
		"ROOT_CAUSE:",
		"SEVERITY:",
		"EXIT_TYPE:",
		"ACTION_REQUIRED:",
		"BUG_FIXABLE:",
		"RECOMMENDATIONS:",
		"FOLLOW_UP:",
	}
	for _, field := range fields {
		if !strings.Contains(prompt, field) {
			t.Errorf("output format missing %q", field)
		}
	}
}

func TestBuildPass1Prompt_DoesNotContainConfidence(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildPass1Prompt(bundle)

	if strings.Contains(prompt, "CONFIDENCE:") {
		t.Error("Pass 1 prompt should not contain CONFIDENCE: field")
	}
}

func TestBuildPass1Prompt_SourceCode(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	bundle.SourceCode["main.go"] = "package main\n\nfunc main() {}"

	prompt := BuildPass1Prompt(bundle)
	if !strings.Contains(prompt, "Source Code Context") {
		t.Error("prompt should contain source code section header")
	}
	if !strings.Contains(prompt, "File: main.go") {
		t.Error("prompt should contain filename")
	}
	if !strings.Contains(prompt, "package main") {
		t.Error("prompt should contain file content")
	}
}

func TestBuildPass1Prompt_TruncatesLongLogs(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	bundle.ErrorLogs = strings.Repeat("x", 9000)

	prompt := BuildPass1Prompt(bundle)
	if !strings.Contains(prompt, "... [truncated]") {
		t.Error("long error logs should be truncated")
	}
}

// --- BuildDeepPassPrompt tests ---

func TestBuildDeepPassPrompt_ContainsPassNumber(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildDeepPassPrompt(bundle, nil, nil, 2)
	if !strings.Contains(prompt, "Pass 2") {
		t.Error("deep pass prompt should contain 'Pass 2'")
	}
}

func TestBuildDeepPassPrompt_ContainsPreviousDiagnosis(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prev := &LLMAnalysis{
		Summary:   "Pod OOMKilled due to memory leak in payment handler",
		RootCause: "Unbounded cache in PaymentService.processPayment()",
	}

	prompt := BuildDeepPassPrompt(bundle, prev, nil, 2)
	if !strings.Contains(prompt, "PREVIOUS DIAGNOSIS") {
		t.Error("prompt should contain PREVIOUS DIAGNOSIS header")
	}
	if !strings.Contains(prompt, prev.Summary) {
		t.Error("prompt should contain previous summary")
	}
	if !strings.Contains(prompt, prev.RootCause) {
		t.Error("prompt should contain previous root cause")
	}
}

func TestBuildDeepPassPrompt_ContainsDeepEvidence(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	deep := &DeepEvidence{
		TraceLogs: map[string]string{
			"trace-abc": "2024-01-15T10:00:01Z ERROR payment failed",
		},
		ExtraLogs:        "additional log lines here",
		ExtraSourceFiles: map[string]string{"handler.go": "func processPayment() {}"},
	}

	prompt := BuildDeepPassPrompt(bundle, nil, deep, 2)
	if !strings.Contains(prompt, "trace-abc") {
		t.Error("prompt should contain trace ID")
	}
	if !strings.Contains(prompt, "additional log lines here") {
		t.Error("prompt should contain extra logs")
	}
	if !strings.Contains(prompt, "handler.go") {
		t.Error("prompt should contain extra source file name")
	}
	if !strings.Contains(prompt, "func processPayment() {}") {
		t.Error("prompt should contain extra source file content")
	}
}

func TestBuildDeepPassPrompt_ContainsConfidence(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildDeepPassPrompt(bundle, nil, nil, 2)
	if !strings.Contains(prompt, "CONFIDENCE:") {
		t.Error("deep pass prompt should contain CONFIDENCE: field")
	}
}

func TestBuildDeepPassPrompt_AbbreviatedOriginalEvidence(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	bundle.ErrorLogs = strings.Repeat("E", 5000)

	pass1Prompt := BuildPass1Prompt(bundle)
	deepPrompt := BuildDeepPassPrompt(bundle, nil, nil, 2)

	pass1ErrLen := len(pass1Prompt)
	deepErrLen := len(deepPrompt)

	if deepErrLen >= pass1ErrLen {
		t.Error("deep pass original evidence should be abbreviated (shorter than Pass 1)")
	}
}

func TestBuildDeepPassPrompt_NilPreviousAnalysis(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildDeepPassPrompt(bundle, nil, nil, 2)
	if strings.Contains(prompt, "PREVIOUS DIAGNOSIS") {
		t.Error("prompt should not contain PREVIOUS DIAGNOSIS when previousAnalysis is nil")
	}
}

func TestBuildDeepPassPrompt_NilDeepEvidence(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle()
	prompt := BuildDeepPassPrompt(bundle, nil, nil, 2)
	if prompt == "" {
		t.Error("prompt should not be empty with nil deep evidence")
	}
	if strings.Contains(prompt, "NEW EVIDENCE (DEEP PASS)") {
		t.Error("prompt should not contain deep evidence header when deep evidence is nil")
	}
}

// --- truncate tests ---

func TestTruncate_Short(t *testing.T) {
	t.Parallel()
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("truncate(%q, 10) = %q; want %q", "hello", got, "hello")
	}
}

func TestTruncate_Exact(t *testing.T) {
	t.Parallel()
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("truncate(%q, 5) = %q; want %q", "hello", got, "hello")
	}
}

func TestTruncate_Long(t *testing.T) {
	t.Parallel()
	input := strings.Repeat("a", 20)
	got := truncate(input, 10)
	want := strings.Repeat("a", 10) + "\n... [truncated]"
	if got != want {
		t.Errorf("truncate(20 chars, 10) = %q; want %q", got, want)
	}
}

// --- BuildEvidenceBundleFromCollected tests ---

func TestBuildEvidenceBundle_LogsBySubKind(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	tests := []struct {
		name    string
		subKind string
		field   string
	}{
		{"error goes to ErrorLogs", "error", "ErrorLogs"},
		{"previous goes to PreviousLogs", "previous", "PreviousLogs"},
		{"current goes to CurrentLogs", "current", "CurrentLogs"},
		{"crash_signal goes to CrashSignalLogs", "crash_signal", "CrashSignalLogs"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev := []contracts.Evidence{
				{
					ID:      "ev-1",
					Kind:    contracts.EvidenceLog,
					Summary: "log content for " + tc.subKind,
					Attributes: map[string]string{
						"sub_kind": tc.subKind,
					},
				},
			}

			bundle := BuildEvidenceBundleFromCollected(alert, ev)
			var got string
			switch tc.field {
			case "ErrorLogs":
				got = bundle.ErrorLogs
			case "PreviousLogs":
				got = bundle.PreviousLogs
			case "CurrentLogs":
				got = bundle.CurrentLogs
			case "CrashSignalLogs":
				got = bundle.CrashSignalLogs
			}
			if !strings.Contains(got, "log content for "+tc.subKind) {
				t.Errorf("%s should contain evidence summary; got %q", tc.field, got)
			}
		})
	}
}

func TestBuildEvidenceBundle_K8sStateBySubKind(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	tests := []struct {
		name    string
		subKind string
		field   string
	}{
		{"pod_status goes to PodStatus", "pod_status", "PodStatus"},
		{"pod_events goes to PodEvents", "pod_events", "PodEvents"},
		{"deployment goes to DeploymentInfo", "deployment", "DeploymentInfo"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev := []contracts.Evidence{
				{
					ID:      "ev-1",
					Kind:    contracts.EvidenceK8sState,
					Summary: "k8s content for " + tc.subKind,
					Attributes: map[string]string{
						"sub_kind": tc.subKind,
					},
				},
			}

			bundle := BuildEvidenceBundleFromCollected(alert, ev)
			var got string
			switch tc.field {
			case "PodStatus":
				got = bundle.PodStatus
			case "PodEvents":
				got = bundle.PodEvents
			case "DeploymentInfo":
				got = bundle.DeploymentInfo
			}
			if !strings.Contains(got, "k8s content for "+tc.subKind) {
				t.Errorf("%s should contain evidence summary; got %q", tc.field, got)
			}
		})
	}
}

func TestBuildEvidenceBundle_Metrics(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	ev := []contracts.Evidence{
		{
			ID:         "ev-m1",
			Kind:       contracts.EvidenceMetric,
			Summary:    "cpu_usage=85%",
			Attributes: map[string]string{},
		},
	}

	bundle := BuildEvidenceBundleFromCollected(alert, ev)
	if !strings.Contains(bundle.MetricSummary, "cpu_usage=85%") {
		t.Errorf("MetricSummary should contain metric evidence; got %q", bundle.MetricSummary)
	}
}

func TestBuildEvidenceBundle_Deploy(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	ev := []contracts.Evidence{
		{
			ID:         "ev-d1",
			Kind:       contracts.EvidenceDeploy,
			Summary:    "deployed v2.3.1 at 10:00",
			Attributes: map[string]string{},
		},
	}

	bundle := BuildEvidenceBundleFromCollected(alert, ev)
	if !strings.Contains(bundle.DeploymentInfo, "deployed v2.3.1 at 10:00") {
		t.Errorf("DeploymentInfo should contain deploy evidence; got %q", bundle.DeploymentInfo)
	}
}

func TestBuildEvidenceBundle_GitChange(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	ev := []contracts.Evidence{
		{
			ID:      "ev-g1",
			Kind:    contracts.EvidenceGitChange,
			Summary: "func main() { panic(\"oops\") }",
			Attributes: map[string]string{
				"file_path": "main.go",
			},
		},
	}

	bundle := BuildEvidenceBundleFromCollected(alert, ev)
	content, ok := bundle.SourceCode["main.go"]
	if !ok {
		t.Fatal("SourceCode should have key 'main.go'")
	}
	if !strings.Contains(content, "panic") {
		t.Errorf("SourceCode[main.go] should contain file content; got %q", content)
	}
}

func TestBuildEvidenceBundle_DefaultLogKind(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	ev := []contracts.Evidence{
		{
			ID:         "ev-l1",
			Kind:       contracts.EvidenceLog,
			Summary:    "default log entry",
			Attributes: map[string]string{},
		},
	}

	bundle := BuildEvidenceBundleFromCollected(alert, ev)
	if !strings.Contains(bundle.CurrentLogs, "default log entry") {
		t.Errorf("logs with no sub_kind should go to CurrentLogs; got %q", bundle.CurrentLogs)
	}
}

func TestBuildEvidenceBundle_AppendMultiple(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	ev := []contracts.Evidence{
		{
			ID:      "ev-1",
			Kind:    contracts.EvidenceLog,
			Summary: "first error",
			Attributes: map[string]string{
				"sub_kind": "error",
			},
		},
		{
			ID:      "ev-2",
			Kind:    contracts.EvidenceLog,
			Summary: "second error",
			Attributes: map[string]string{
				"sub_kind": "error",
			},
		},
	}

	bundle := BuildEvidenceBundleFromCollected(alert, ev)
	if !strings.Contains(bundle.ErrorLogs, "first error") {
		t.Error("ErrorLogs should contain first error")
	}
	if !strings.Contains(bundle.ErrorLogs, "second error") {
		t.Error("ErrorLogs should contain second error")
	}
}

func TestBuildEvidenceBundle_EmptyEvidence(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	bundle := BuildEvidenceBundleFromCollected(alert, nil)

	if bundle.PodStatus != "" {
		t.Error("PodStatus should be empty")
	}
	if bundle.PodEvents != "" {
		t.Error("PodEvents should be empty")
	}
	if bundle.ErrorLogs != "" {
		t.Error("ErrorLogs should be empty")
	}
	if bundle.CurrentLogs != "" {
		t.Error("CurrentLogs should be empty")
	}
	if bundle.MetricSummary != "" {
		t.Error("MetricSummary should be empty")
	}
	if bundle.DeploymentInfo != "" {
		t.Error("DeploymentInfo should be empty")
	}
	if len(bundle.SourceCode) != 0 {
		t.Error("SourceCode should be empty")
	}
}

func TestBuildEvidenceBundle_UsesBodyRefWhenSummaryEmpty(t *testing.T) {
	t.Parallel()
	alert := newTestAlert()
	ev := []contracts.Evidence{
		{
			ID:      "ev-1",
			Kind:    contracts.EvidenceLog,
			Summary: "",
			BodyRef: "s3://bucket/evidence/ev-1.txt body content",
			Attributes: map[string]string{
				"sub_kind": "error",
			},
		},
	}

	bundle := BuildEvidenceBundleFromCollected(alert, ev)
	if !strings.Contains(bundle.ErrorLogs, "s3://bucket/evidence/ev-1.txt body content") {
		t.Errorf("should use BodyRef when Summary is empty; got %q", bundle.ErrorLogs)
	}
}

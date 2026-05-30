package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

// --- Mocks ---

type mockCollectorSet struct {
	collectFunc func(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error)
	calls       []contracts.CollectRequest
}

func (m *mockCollectorSet) CollectAll(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	m.calls = append(m.calls, req)
	return m.collectFunc(ctx, req)
}

type mockGitProvider struct {
	fetchFunc   func(ctx context.Context, repo string, paths []string) (map[string]string, error)
	resolveFunc func(workload string) (string, bool)
}

func (m *mockGitProvider) FetchFiles(ctx context.Context, repo string, paths []string) (map[string]string, error) {
	return m.fetchFunc(ctx, repo, paths)
}

func (m *mockGitProvider) ResolveRepo(workload string) (string, bool) {
	return m.resolveFunc(workload)
}

func newFollowUpTestAlert() contracts.NormalizedAlert {
	return contracts.NormalizedAlert{
		ID:       "alert-fu-001",
		Source:   "prometheus",
		TenantID: "tenant-1",
		Status:   contracts.AlertStatusFiring,
		Severity: contracts.SeverityCritical,
		Title:    "HighErrorRate",
		Summary:  "Error rate above 5%",
		StartsAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Labels: map[string]string{
			"service":   "payment-svc",
			"namespace": "production",
		},
	}
}

func testTargets() []contracts.TargetRef {
	return []contracts.TargetRef{
		{Kind: "k8s.pod", Namespace: "production", Name: "payment-svc-abc"},
	}
}

// --- FollowUpExecutor tests ---

func TestFollowUpExecutor_TraceLogs(t *testing.T) {
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
			traceID := req.Alert.Labels["trace_id"]
			return []contracts.Evidence{
				{ID: "ev-" + traceID, Kind: contracts.EvidenceLog, Summary: "log line for " + traceID},
			}, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "TRACE_LOGS", Value: "trace1, trace2"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.calls) != 2 {
		t.Fatalf("expected 2 collector calls; got %d", len(collector.calls))
	}
	if _, ok := deep.TraceLogs["trace1"]; !ok {
		t.Error("TraceLogs should contain 'trace1'")
	}
	if _, ok := deep.TraceLogs["trace2"]; !ok {
		t.Error("TraceLogs should contain 'trace2'")
	}
}

func TestFollowUpExecutor_TimeWindowLogs(t *testing.T) {
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
			return []contracts.Evidence{
				{ID: "ev-tw", Kind: contracts.EvidenceLog, Summary: "time window log line"},
			}, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "TIME_WINDOW_LOGS", Value: "2024-01-15T10:00:00Z/2024-01-15T10:05:00Z"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.calls) != 1 {
		t.Fatalf("expected 1 collector call; got %d", len(collector.calls))
	}

	req := collector.calls[0]
	expectedFrom := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	expectedTo := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)
	if !req.TimeFrom.Equal(expectedFrom) {
		t.Errorf("TimeFrom = %v; want %v", req.TimeFrom, expectedFrom)
	}
	if !req.TimeTo.Equal(expectedTo) {
		t.Errorf("TimeTo = %v; want %v", req.TimeTo, expectedTo)
	}
	if !strings.Contains(deep.ExtraLogs, "time window log line") {
		t.Errorf("ExtraLogs should contain log; got %q", deep.ExtraLogs)
	}
}

func TestFollowUpExecutor_TimeWindowLogs_InvalidFormat(t *testing.T) {
	before := time.Now().UTC()
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			return []contracts.Evidence{
				{ID: "ev-tw", Kind: contracts.EvidenceLog, Summary: "fallback log"},
			}, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "TIME_WINDOW_LOGS", Value: "not-a-valid-time"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now().UTC()

	req := collector.calls[0]
	expectedFromEarliest := before.Add(-31 * time.Minute)
	if req.TimeFrom.Before(expectedFromEarliest) {
		t.Errorf("invalid format should fall back to ~30m ago; TimeFrom = %v", req.TimeFrom)
	}
	if req.TimeTo.After(after.Add(time.Second)) {
		t.Errorf("invalid format should fall back to ~now; TimeTo = %v", req.TimeTo)
	}
	if !strings.Contains(deep.ExtraLogs, "fallback log") {
		t.Error("should still collect logs with fallback time window")
	}
}

func TestFollowUpExecutor_PodEvents(t *testing.T) {
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			return []contracts.Evidence{
				{ID: "ev-pe1", Kind: contracts.EvidenceEvent, Summary: "BackOff restarting"},
				{ID: "ev-pe2", Kind: contracts.EvidenceK8sState, Summary: "pod status changed"},
				{ID: "ev-pe3", Kind: contracts.EvidenceLog, Summary: "this is a log, not event"},
			}, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "POD_EVENTS", Value: "all"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(deep.ExtraPodEvents, "BackOff restarting") {
		t.Error("ExtraPodEvents should contain event evidence")
	}
	if !strings.Contains(deep.ExtraPodEvents, "pod status changed") {
		t.Error("ExtraPodEvents should contain k8s_state evidence")
	}
	if strings.Contains(deep.ExtraPodEvents, "this is a log") {
		t.Error("ExtraPodEvents should not contain log evidence")
	}
}

func TestFollowUpExecutor_GitHubFiles(t *testing.T) {
	gitProv := &mockGitProvider{
		resolveFunc: func(workload string) (string, bool) {
			if workload == "payment-svc" {
				return "payment-service", true
			}
			return "", false
		},
		fetchFunc: func(_ context.Context, repo string, paths []string) (map[string]string, error) {
			result := make(map[string]string)
			for _, p := range paths {
				result[p] = "content of " + p
			}
			return result, nil
		},
	}

	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			return nil, nil
		},
	}

	exec := NewFollowUpExecutor(collector, gitProv, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "GITHUB_FILES", Value: "cmd/main.go, internal/handler.go"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deep.ExtraSourceFiles) != 2 {
		t.Fatalf("expected 2 files; got %d", len(deep.ExtraSourceFiles))
	}
	if deep.ExtraSourceFiles["cmd/main.go"] != "content of cmd/main.go" {
		t.Errorf("unexpected content for cmd/main.go: %q", deep.ExtraSourceFiles["cmd/main.go"])
	}
	if deep.ExtraSourceFiles["internal/handler.go"] != "content of internal/handler.go" {
		t.Errorf("unexpected content for internal/handler.go: %q", deep.ExtraSourceFiles["internal/handler.go"])
	}
}

func TestFollowUpExecutor_GitHubFiles_NoProvider(t *testing.T) {
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			return nil, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "GITHUB_FILES", Value: "main.go"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deep.ExtraSourceFiles) != 0 {
		t.Errorf("expected empty ExtraSourceFiles with nil git provider; got %d files", len(deep.ExtraSourceFiles))
	}
}

func TestFollowUpExecutor_GitHubFiles_NoRepoMapping(t *testing.T) {
	gitProv := &mockGitProvider{
		resolveFunc: func(_ string) (string, bool) {
			return "", false
		},
		fetchFunc: func(_ context.Context, _ string, _ []string) (map[string]string, error) {
			t.Error("FetchFiles should not be called when ResolveRepo returns false")
			return nil, nil
		},
	}

	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			return nil, nil
		},
	}

	exec := NewFollowUpExecutor(collector, gitProv, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "GITHUB_FILES", Value: "main.go"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deep.ExtraSourceFiles) != 0 {
		t.Error("expected empty ExtraSourceFiles when repo mapping not found")
	}
}

func TestFollowUpExecutor_LogQuery(t *testing.T) {
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
			if req.Alert.Labels["custom_query"] == "" {
				t.Error("expected custom_query label to be set")
			}
			return []contracts.Evidence{
				{ID: "ev-lq", Kind: contracts.EvidenceLog, Summary: "custom query result"},
			}, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "LOG_QUERY", Value: `{namespace="production"} |= "error"`},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(deep.CustomQueryResults, "custom query result") {
		t.Errorf("CustomQueryResults should contain query results; got %q", deep.CustomQueryResults)
	}
}

func TestFollowUpExecutor_UnknownTool(t *testing.T) {
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			t.Error("collector should not be called for unknown tool")
			return nil, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "NONEXISTENT_TOOL", Value: "some value"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deep == nil {
		t.Fatal("deep evidence should not be nil")
	}
}

func TestFollowUpExecutor_MultipleFollowUps(t *testing.T) {
	callIdx := 0
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			callIdx++
			switch callIdx {
			case 1:
				return []contracts.Evidence{
					{ID: "ev-trace", Kind: contracts.EvidenceLog, Summary: "trace log"},
				}, nil
			case 2:
				return []contracts.Evidence{
					{ID: "ev-event", Kind: contracts.EvidenceEvent, Summary: "pod event"},
				}, nil
			case 3:
				return []contracts.Evidence{
					{ID: "ev-query", Kind: contracts.EvidenceLog, Summary: "query result"},
				}, nil
			default:
				return nil, nil
			}
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "TRACE_LOGS", Value: "trace-xyz"},
		{Tool: "POD_EVENTS", Value: "all"},
		{Tool: "LOG_QUERY", Value: `{app="test"}`},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deep.TraceLogs) == 0 {
		t.Error("TraceLogs should be populated")
	}
	if deep.ExtraPodEvents == "" {
		t.Error("ExtraPodEvents should be populated")
	}
	if deep.CustomQueryResults == "" {
		t.Error("CustomQueryResults should be populated")
	}
}

func TestFollowUpExecutor_CollectorError(t *testing.T) {
	callIdx := 0
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			callIdx++
			if callIdx == 1 {
				return nil, context.DeadlineExceeded
			}
			return []contracts.Evidence{
				{ID: "ev-ok", Kind: contracts.EvidenceEvent, Summary: "event data"},
			}, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()
	followUps := []FollowUpQuery{
		{Tool: "TRACE_LOGS", Value: "trace-fail"},
		{Tool: "POD_EVENTS", Value: "all"},
	}

	deep, err := exec.Execute(context.Background(), followUps, alert, testTargets())
	if err != nil {
		t.Fatalf("executor should not return error; got %v", err)
	}

	if callIdx < 2 {
		t.Errorf("expected at least 2 collector calls; got %d", callIdx)
	}
	if deep.ExtraPodEvents == "" {
		t.Error("pod events should still be collected after trace error")
	}
}

func TestFollowUpExecutor_EmptyFollowUps(t *testing.T) {
	collector := &mockCollectorSet{
		collectFunc: func(_ context.Context, _ contracts.CollectRequest) ([]contracts.Evidence, error) {
			t.Error("collector should not be called for empty follow-ups")
			return nil, nil
		},
	}

	exec := NewFollowUpExecutor(collector, nil, zap.NewNop())
	alert := newFollowUpTestAlert()

	deep, err := exec.Execute(context.Background(), nil, alert, testTargets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deep == nil {
		t.Fatal("should return non-nil DeepEvidence")
	}
	if len(deep.TraceLogs) != 0 {
		t.Error("TraceLogs should be empty")
	}
	if deep.ExtraLogs != "" {
		t.Error("ExtraLogs should be empty")
	}
	if deep.ExtraPodEvents != "" {
		t.Error("ExtraPodEvents should be empty")
	}
	if len(deep.ExtraSourceFiles) != 0 {
		t.Error("ExtraSourceFiles should be empty")
	}
	if deep.CustomQueryResults != "" {
		t.Error("CustomQueryResults should be empty")
	}
}

// --- parseCSV tests ---

func TestParseCSV_Basic(t *testing.T) {
	got := parseCSV("a, b, c")
	if len(got) != 3 {
		t.Fatalf("expected 3 items; got %d", len(got))
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("parseCSV(\"a, b, c\") = %v; want [a b c]", got)
	}
}

func TestParseCSV_Whitespace(t *testing.T) {
	got := parseCSV("  a , b  ")
	if len(got) != 2 {
		t.Fatalf("expected 2 items; got %d", len(got))
	}
	if got[0] != "a" || got[1] != "b" {
		t.Errorf("parseCSV(\"  a , b  \") = %v; want [a b]", got)
	}
}

func TestParseCSV_Empty(t *testing.T) {
	got := parseCSV("")
	if len(got) != 0 {
		t.Errorf("parseCSV(\"\") should return empty; got %v", got)
	}
}

func TestParseCSV_SingleValue(t *testing.T) {
	got := parseCSV("abc")
	if len(got) != 1 || got[0] != "abc" {
		t.Errorf("parseCSV(\"abc\") = %v; want [abc]", got)
	}
}

// --- parseTimeWindow tests ---

func TestParseTimeWindow_ValidRFC3339(t *testing.T) {
	from, to := parseTimeWindow("2024-01-15T10:00:00Z/2024-01-15T10:05:00Z")
	expectedFrom := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	expectedTo := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)

	if !from.Equal(expectedFrom) {
		t.Errorf("from = %v; want %v", from, expectedFrom)
	}
	if !to.Equal(expectedTo) {
		t.Errorf("to = %v; want %v", to, expectedTo)
	}
}

func TestParseTimeWindow_InvalidFormat(t *testing.T) {
	before := time.Now().UTC()
	from, to := parseTimeWindow("garbage")
	after := time.Now().UTC()

	expectedEarliest := before.Add(-31 * time.Minute)
	if from.Before(expectedEarliest) {
		t.Errorf("invalid format: from should be ~30m ago; got %v", from)
	}
	if to.After(after.Add(time.Second)) {
		t.Errorf("invalid format: to should be ~now; got %v", to)
	}
}

func TestParseTimeWindow_PartialInvalid(t *testing.T) {
	before := time.Now().UTC()
	from, to := parseTimeWindow("2024-01-15T10:00:00Z/garbage")
	after := time.Now().UTC()

	expectedFrom := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !from.Equal(expectedFrom) {
		t.Errorf("from should be parsed; got %v, want %v", from, expectedFrom)
	}
	if to.Before(before) || to.After(after.Add(time.Second)) {
		t.Errorf("to should default to ~now; got %v", to)
	}
}

// --- resolveWorkload tests ---

func TestResolveWorkload_ServiceLabel(t *testing.T) {
	alert := contracts.NormalizedAlert{
		ID: "a1",
		Labels: map[string]string{
			"service": "payment-svc",
			"app":     "payment-app",
		},
	}
	got := resolveWorkload(alert)
	if got != "payment-svc" {
		t.Errorf("resolveWorkload with service label = %q; want %q", got, "payment-svc")
	}
}

func TestResolveWorkload_AppLabel(t *testing.T) {
	alert := contracts.NormalizedAlert{
		ID: "a2",
		Labels: map[string]string{
			"app": "order-api",
		},
	}
	got := resolveWorkload(alert)
	if got != "order-api" {
		t.Errorf("resolveWorkload with app label = %q; want %q", got, "order-api")
	}
}

func TestResolveWorkload_NoLabel(t *testing.T) {
	alert := contracts.NormalizedAlert{
		ID:     "a3",
		Labels: map[string]string{},
	}
	got := resolveWorkload(alert)
	if got != "unknown-a3" {
		t.Errorf("resolveWorkload with no labels = %q; want %q", got, "unknown-a3")
	}
}

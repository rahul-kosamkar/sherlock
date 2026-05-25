package prometheus

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type mockPromAPI struct {
	result   model.Value
	warnings promv1.Warnings
	err      error
}

func (m *mockPromAPI) Alerts(ctx context.Context) (promv1.AlertsResult, error) {
	return promv1.AlertsResult{}, nil
}

func (m *mockPromAPI) AlertManagers(ctx context.Context) (promv1.AlertManagersResult, error) {
	return promv1.AlertManagersResult{}, nil
}

func (m *mockPromAPI) CleanTombstones(ctx context.Context) error {
	return nil
}

func (m *mockPromAPI) Config(ctx context.Context) (promv1.ConfigResult, error) {
	return promv1.ConfigResult{}, nil
}

func (m *mockPromAPI) DeleteSeries(ctx context.Context, matches []string, startTime, endTime time.Time) error {
	return nil
}

func (m *mockPromAPI) Flags(ctx context.Context) (promv1.FlagsResult, error) {
	return promv1.FlagsResult{}, nil
}

func (m *mockPromAPI) LabelNames(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...promv1.Option) ([]string, promv1.Warnings, error) {
	return nil, nil, nil
}

func (m *mockPromAPI) LabelValues(ctx context.Context, label string, matches []string, startTime, endTime time.Time, opts ...promv1.Option) (model.LabelValues, promv1.Warnings, error) {
	return nil, nil, nil
}

func (m *mockPromAPI) Query(ctx context.Context, query string, ts time.Time, opts ...promv1.Option) (model.Value, promv1.Warnings, error) {
	return nil, nil, nil
}

func (m *mockPromAPI) QueryRange(ctx context.Context, query string, r promv1.Range, opts ...promv1.Option) (model.Value, promv1.Warnings, error) {
	return m.result, m.warnings, m.err
}

func (m *mockPromAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]promv1.ExemplarQueryResult, error) {
	return nil, nil
}

func (m *mockPromAPI) Buildinfo(ctx context.Context) (promv1.BuildinfoResult, error) {
	return promv1.BuildinfoResult{}, nil
}

func (m *mockPromAPI) Runtimeinfo(ctx context.Context) (promv1.RuntimeinfoResult, error) {
	return promv1.RuntimeinfoResult{}, nil
}

func (m *mockPromAPI) Series(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...promv1.Option) ([]model.LabelSet, promv1.Warnings, error) {
	return nil, nil, nil
}

func (m *mockPromAPI) Snapshot(ctx context.Context, skipHead bool) (promv1.SnapshotResult, error) {
	return promv1.SnapshotResult{}, nil
}

func (m *mockPromAPI) Rules(ctx context.Context) (promv1.RulesResult, error) {
	return promv1.RulesResult{}, nil
}

func (m *mockPromAPI) Targets(ctx context.Context) (promv1.TargetsResult, error) {
	return promv1.TargetsResult{}, nil
}

func (m *mockPromAPI) TargetsMetadata(ctx context.Context, matchTarget, metric, limit string) ([]promv1.MetricMetadata, error) {
	return nil, nil
}

func (m *mockPromAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]promv1.Metadata, error) {
	return nil, nil
}

func (m *mockPromAPI) TSDB(ctx context.Context, opts ...promv1.Option) (promv1.TSDBResult, error) {
	return promv1.TSDBResult{}, nil
}

func (m *mockPromAPI) WalReplay(ctx context.Context) (promv1.WalReplayStatus, error) {
	return promv1.WalReplayStatus{}, nil
}

func newTestCollector(api promv1.API) *Collector {
	return &Collector{api: api, log: zap.NewNop()}
}

func TestCollector_Name(t *testing.T) {
	c := newTestCollector(&mockPromAPI{})
	if got := c.Name(); got != "prometheus" {
		t.Fatalf("Name() = %q, want %q", got, "prometheus")
	}
}

func TestBuildQueries_WithService(t *testing.T) {
	target := contracts.TargetRef{Namespace: "prod", Name: "web"}
	labels := map[string]string{"service": "foo"}

	queries := buildQueries(target, labels)
	if len(queries) != 4 {
		t.Fatalf("expected 4 queries, got %d", len(queries))
	}

	hasErrorRate := false
	for _, q := range queries {
		if q.name == "error_rate" {
			hasErrorRate = true
			if !strings.Contains(q.expr, `service="foo"`) {
				t.Errorf("error_rate query missing service selector: %s", q.expr)
			}
		}
	}
	if !hasErrorRate {
		t.Fatal("expected error_rate query when service label is present")
	}
}

func TestBuildQueries_WithJob(t *testing.T) {
	target := contracts.TargetRef{Namespace: "prod", Name: "web"}
	labels := map[string]string{"job": "bar"}

	queries := buildQueries(target, labels)
	if len(queries) != 4 {
		t.Fatalf("expected 4 queries, got %d", len(queries))
	}

	hasErrorRate := false
	for _, q := range queries {
		if q.name == "error_rate" {
			hasErrorRate = true
			if !strings.Contains(q.expr, `service="bar"`) {
				t.Errorf("error_rate query missing service selector: %s", q.expr)
			}
		}
	}
	if !hasErrorRate {
		t.Fatal("expected error_rate query when job label is present")
	}
}

func TestBuildQueries_NoService(t *testing.T) {
	target := contracts.TargetRef{Namespace: "prod", Name: "web"}
	labels := map[string]string{}

	queries := buildQueries(target, labels)
	if len(queries) != 3 {
		t.Fatalf("expected 3 queries, got %d", len(queries))
	}

	for _, q := range queries {
		if q.name == "error_rate" {
			t.Fatal("should not include error_rate without service/job label")
		}
	}
}

func TestFirstLabel_Found(t *testing.T) {
	labels := map[string]string{"service": "foo", "job": "bar"}
	got := firstLabel(labels, "service", "job")
	if got != "foo" {
		t.Fatalf("firstLabel() = %q, want %q", got, "foo")
	}
}

func TestFirstLabel_NotFound(t *testing.T) {
	labels := map[string]string{"region": "us-east-1"}
	got := firstLabel(labels, "service", "job")
	if got != "" {
		t.Fatalf("firstLabel() = %q, want empty", got)
	}
}

func makeMatrix(values ...float64) model.Matrix {
	now := time.Now()
	pairs := make([]model.SamplePair, len(values))
	for i, v := range values {
		pairs[i] = model.SamplePair{
			Timestamp: model.Time(now.Add(time.Duration(i-len(values)) * time.Minute).UnixMilli()),
			Value:     model.SampleValue(v),
		}
	}
	return model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{"__name__": "test_metric"},
			Values: pairs,
		},
	}
}

func TestAnalyzeMatrix_Anomaly(t *testing.T) {
	matrix := makeMatrix(0.1, 0.1, 0.1, 0.1, 0.5)
	summary, score := analyzeMatrix("cpu_usage", matrix)
	if score != scoreAnomaly {
		t.Fatalf("score = %f, want %f", score, scoreAnomaly)
	}
	if !strings.Contains(summary, "anomaly") {
		t.Fatalf("summary %q should contain 'anomaly'", summary)
	}
}

func TestAnalyzeMatrix_Elevated(t *testing.T) {
	matrix := makeMatrix(0.10, 0.10, 0.10, 0.10, 0.19)
	summary, score := analyzeMatrix("cpu_usage", matrix)
	if score != scoreElevated {
		t.Fatalf("score = %f, want %f", score, scoreElevated)
	}
	if !strings.Contains(summary, "elevated") {
		t.Fatalf("summary %q should contain 'elevated'", summary)
	}
}

func TestAnalyzeMatrix_Normal(t *testing.T) {
	matrix := makeMatrix(0.1, 0.1, 0.1, 0.1, 0.1)
	summary, score := analyzeMatrix("cpu_usage", matrix)
	if score != scoreNormal {
		t.Fatalf("score = %f, want %f", score, scoreNormal)
	}
	if !strings.Contains(summary, "normal") {
		t.Fatalf("summary %q should contain 'normal'", summary)
	}
}

func TestAnalyzeMatrix_ZeroAvg_WithSpike(t *testing.T) {
	// avg == 0 with last > 0 is unreachable since avg includes last.
	// Verify that a spike from near-zero is detected as an anomaly.
	matrix := makeMatrix(0, 0, 0, 0, 0.5)
	summary, score := analyzeMatrix("restart_count", matrix)
	if score != scoreAnomaly {
		t.Fatalf("score = %f, want %f", score, scoreAnomaly)
	}
	if !strings.Contains(summary, "anomaly") {
		t.Fatalf("summary %q should contain 'anomaly'", summary)
	}
}

func TestAnalyzeMatrix_ZeroAvg_NoSpike(t *testing.T) {
	matrix := makeMatrix(0, 0, 0, 0, 0)
	_, score := analyzeMatrix("cpu_usage", matrix)
	if score != scoreNormal {
		t.Fatalf("score = %f, want %f (normal for all-zero)", score, scoreNormal)
	}
}

func TestAnalyzeMatrix_TooFewValues(t *testing.T) {
	now := time.Now()
	matrix := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{"__name__": "test"},
			Values: []model.SamplePair{
				{Timestamp: model.Time(now.UnixMilli()), Value: 0.5},
			},
		},
	}
	_, score := analyzeMatrix("cpu_usage", matrix)
	if score != scoreNormal {
		t.Fatalf("score = %f, want %f (normal for too few values)", score, scoreNormal)
	}
}

func baseRequest() contracts.CollectRequest {
	return contracts.CollectRequest{
		InvestigationID: "inv-test",
		Alert: contracts.NormalizedAlert{
			Labels: map[string]string{"service": "web-svc"},
		},
		Targets: []contracts.TargetRef{
			{Kind: "k8s.deployment", Namespace: "prod", Name: "web"},
		},
		TimeFrom: time.Now().Add(-1 * time.Hour),
		TimeTo:   time.Now(),
	}
}

func TestCollect_WithResults(t *testing.T) {
	matrix := makeMatrix(0.1, 0.1, 0.1, 0.1, 0.5)
	api := &mockPromAPI{result: matrix}
	c := newTestCollector(api)
	req := baseRequest()

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) == 0 {
		t.Fatal("expected evidence from Prometheus results")
	}
	for _, e := range ev {
		if e.Source != "prometheus" {
			t.Errorf("evidence source = %q, want %q", e.Source, "prometheus")
		}
		if e.Kind != contracts.EvidenceMetric {
			t.Errorf("evidence kind = %q, want %q", e.Kind, contracts.EvidenceMetric)
		}
	}
}

func TestCollect_QueryError(t *testing.T) {
	api := &mockPromAPI{err: fmt.Errorf("connection refused")}
	c := newTestCollector(api)
	req := baseRequest()

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence on query error, got %d", len(ev))
	}
}

func TestCollect_LowScore(t *testing.T) {
	matrix := makeMatrix(0.1, 0.1, 0.1, 0.1, 0.1)
	api := &mockPromAPI{result: matrix}
	c := newTestCollector(api)
	req := baseRequest()
	req.Alert.Labels = map[string]string{}

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, e := range ev {
		if e.Score < scoreNormal {
			t.Errorf("evidence with score %f should not appear (below scoreNormal %f)", e.Score, scoreNormal)
		}
	}
}

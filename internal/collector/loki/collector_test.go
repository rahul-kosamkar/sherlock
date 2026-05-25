package loki

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

func newTestCollector(server *httptest.Server) *Collector {
	return &Collector{
		httpClient: server.Client(),
		baseURL:    server.URL,
		log:        zap.NewNop(),
	}
}

func TestCollector_Name(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := newTestCollector(srv)
	if got := c.Name(); got != "loki" {
		t.Fatalf("Name() = %q, want %q", got, "loki")
	}
}

func TestNew_ValidURL(t *testing.T) {
	c, err := New("http://localhost:3100", zap.NewNop())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if c == nil {
		t.Fatal("New() returned nil collector")
	}
}

func TestBuildLogQueries(t *testing.T) {
	target := contracts.TargetRef{
		Namespace: "prod",
		Name:      "api-server",
	}

	queries := buildLogQueries(target)
	if len(queries) != 3 {
		t.Fatalf("expected 3 queries, got %d", len(queries))
	}

	names := map[string]bool{}
	for _, q := range queries {
		names[q.name] = true
		if !strings.Contains(q.expr, `namespace="prod"`) {
			t.Errorf("query %s missing namespace selector: %s", q.name, q.expr)
		}
		if !strings.Contains(q.expr, `app="api-server"`) {
			t.Errorf("query %s missing app selector: %s", q.name, q.expr)
		}
	}

	for _, expected := range []string{"error_logs", "fatal_panic", "stack_traces"} {
		if !names[expected] {
			t.Errorf("missing query %q", expected)
		}
	}
}

func TestBuildLogQueries_NoName(t *testing.T) {
	target := contracts.TargetRef{
		Namespace: "prod",
		Name:      "",
	}

	queries := buildLogQueries(target)
	for _, q := range queries {
		if strings.Contains(q.expr, "app=") {
			t.Errorf("query %s should not have app filter when name is empty: %s", q.name, q.expr)
		}
	}
}

func TestExtractLines(t *testing.T) {
	values := [][]string{
		{"1000000000", "line1"},
		{"2000000000", "line2"},
	}
	got := extractLines(values)
	want := "line1\nline2\n"
	if got != want {
		t.Fatalf("extractLines() = %q, want %q", got, want)
	}
}

func TestExtractLines_ShortPair(t *testing.T) {
	values := [][]string{
		{"1000000000"},
	}
	got := extractLines(values)
	if got != "" {
		t.Fatalf("extractLines() = %q, want empty", got)
	}
}

func TestExtractLines_Empty(t *testing.T) {
	got := extractLines(nil)
	if got != "" {
		t.Fatalf("extractLines() = %q, want empty", got)
	}
}

func TestScoreForContent_Fatal(t *testing.T) {
	if got := scoreForContent("something fatal happened", scoreDefault); got != scoreFatal {
		t.Fatalf("scoreForContent(fatal) = %f, want %f", got, scoreFatal)
	}
}

func TestScoreForContent_Panic(t *testing.T) {
	if got := scoreForContent("goroutine panic: runtime error", scoreDefault); got != scoreFatal {
		t.Fatalf("scoreForContent(panic) = %f, want %f", got, scoreFatal)
	}
}

func TestScoreForContent_OOMKilled(t *testing.T) {
	if got := scoreForContent("container was OOMKilled", scoreDefault); got != scoreFatal {
		t.Fatalf("scoreForContent(oomkilled) = %f, want %f", got, scoreFatal)
	}
}

func TestScoreForContent_Error(t *testing.T) {
	if got := scoreForContent("error connecting to database", scoreDefault); got != scoreError {
		t.Fatalf("scoreForContent(error) = %f, want %f", got, scoreError)
	}
}

func TestScoreForContent_Warning(t *testing.T) {
	if got := scoreForContent("warning: disk usage high", scoreDefault); got != scoreWarning {
		t.Fatalf("scoreForContent(warning) = %f, want %f", got, scoreWarning)
	}
}

func TestScoreForContent_Default(t *testing.T) {
	if got := scoreForContent("all systems operational", scoreDefault); got != scoreDefault {
		t.Fatalf("scoreForContent(default) = %f, want %f", got, scoreDefault)
	}
}

func TestScoreForContent_HigherBasePreserved(t *testing.T) {
	base := 0.9
	got := scoreForContent("error connecting to database", base)
	if got != base {
		t.Fatalf("scoreForContent(high base) = %f, want %f (base preserved)", got, base)
	}
}

func TestParseNanoTimestamp_Valid(t *testing.T) {
	ts, err := parseNanoTimestamp("1609459200000000000")
	if err != nil {
		t.Fatalf("parseNanoTimestamp() error = %v", err)
	}
	want := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	if !ts.Equal(want) {
		t.Fatalf("parseNanoTimestamp() = %v, want %v", ts, want)
	}
}

func TestParseNanoTimestamp_Invalid(t *testing.T) {
	_, err := parseNanoTimestamp("not-a-number")
	if err == nil {
		t.Fatal("parseNanoTimestamp() expected error for non-numeric string")
	}
}

func TestTruncateBody_Short(t *testing.T) {
	s := "short string"
	got := truncateBody(s, 100)
	if got != s {
		t.Fatalf("truncateBody() = %q, want %q", got, s)
	}
}

func TestTruncateBody_Long(t *testing.T) {
	s := strings.Repeat("x", 100)
	got := truncateBody(s, 50)
	if len(got) <= 50 {
		t.Fatal("truncated result should include the truncation marker")
	}
	if !strings.Contains(got, "... truncated ...") {
		t.Fatalf("truncateBody() should contain truncation marker, got %q", got)
	}
	if got[:50] != s[:50] {
		t.Fatal("truncated prefix should match original")
	}
}

func baseRequest() contracts.CollectRequest {
	return contracts.CollectRequest{
		InvestigationID: "inv-test",
		Targets: []contracts.TargetRef{
			{Kind: "service", Namespace: "prod", Name: "api-server"},
		},
		TimeFrom: time.Now().Add(-1 * time.Hour),
		TimeTo:   time.Now(),
	}
}

func lokiSuccessResponse(t *testing.T) []byte {
	t.Helper()
	resp := lokiResponse{
		Status: "success",
		Data: lokiData{
			ResultType: "streams",
			Result: []lokiStream{
				{
					Stream: map[string]string{"namespace": "prod", "app": "api-server"},
					Values: [][]string{
						{"1609459200000000000", "error: connection refused"},
						{"1609459201000000000", "error: timeout exceeded"},
					},
				},
			},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal loki response: %v", err)
	}
	return b
}

func TestCollect_Success(t *testing.T) {
	body := lokiSuccessResponse(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	c := newTestCollector(srv)
	req := baseRequest()

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) == 0 {
		t.Fatal("expected evidence from successful Loki response")
	}
	for _, e := range ev {
		if e.Source != "loki" {
			t.Errorf("evidence source = %q, want %q", e.Source, "loki")
		}
		if e.Kind != contracts.EvidenceLog {
			t.Errorf("evidence kind = %q, want %q", e.Kind, contracts.EvidenceLog)
		}
	}
}

func TestCollect_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	c := newTestCollector(srv)
	req := baseRequest()

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence on server error, got %d", len(ev))
	}
}

func TestCollect_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := newTestCollector(srv)
	req := baseRequest()

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence on invalid JSON, got %d", len(ev))
	}
}

func TestCollect_NonSuccessStatus(t *testing.T) {
	resp := lokiResponse{
		Status: "error",
		Data:   lokiData{},
	}
	body, _ := json.Marshal(resp)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	c := newTestCollector(srv)
	req := baseRequest()

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence on non-success status, got %d", len(ev))
	}
}

func TestCollect_EmptyStreams(t *testing.T) {
	resp := lokiResponse{
		Status: "success",
		Data: lokiData{
			ResultType: "streams",
			Result:     []lokiStream{},
		},
	}
	body, _ := json.Marshal(resp)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	c := newTestCollector(srv)
	req := baseRequest()

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence on empty streams, got %d", len(ev))
	}
}

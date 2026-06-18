package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type mockInvestigationReader struct {
	inv *contracts.Investigation
	err error
}

func (m *mockInvestigationReader) GetByID(_ context.Context, id string) (*contracts.Investigation, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.inv, nil
}

type mockEvidenceReader struct {
	evidence []contracts.Evidence
	err      error
}

func (m *mockEvidenceReader) ListByInvestigation(_ context.Context, investigationID string) ([]contracts.Evidence, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.evidence, nil
}

type mockTimelineReader struct {
	events []contracts.TimelineEvent
	err    error
}

func (m *mockTimelineReader) ListByInvestigation(_ context.Context, investigationID string) ([]contracts.TimelineEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.events, nil
}

type mockHypothesisReader struct {
	hypotheses []contracts.Hypothesis
	err        error
}

func (m *mockHypothesisReader) ListByInvestigation(_ context.Context, investigationID string) ([]contracts.Hypothesis, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.hypotheses, nil
}

func newTestServer(
	invReader *mockInvestigationReader,
	evReader *mockEvidenceReader,
	tlReader *mockTimelineReader,
	hypReader *mockHypothesisReader,
) *Server {
	return NewServer(invReader, evReader, tlReader, hypReader, zap.NewNop())
}

func TestHealth_ReturnsOK(t *testing.T) {
	t.Parallel()
	srv := newTestServer(
		&mockInvestigationReader{},
		&mockEvidenceReader{},
		&mockTimelineReader{},
		&mockHypothesisReader{},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
	if body["service"] != "sherlock" {
		t.Errorf("expected service 'sherlock', got %q", body["service"])
	}
}

func TestGetInvestigation_Found(t *testing.T) {
	t.Parallel()
	inv := &contracts.Investigation{
		ID:       "inv-1",
		TenantID: "tenant-1",
		Status:   contracts.StatusDone,
		Headline: "CPU spike on api-server",
	}
	srv := newTestServer(
		&mockInvestigationReader{inv: inv},
		&mockEvidenceReader{},
		&mockTimelineReader{},
		&mockHypothesisReader{},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/inv-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var result contracts.Investigation
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.ID != "inv-1" {
		t.Errorf("expected ID 'inv-1', got %q", result.ID)
	}
	if result.Headline != "CPU spike on api-server" {
		t.Errorf("expected headline 'CPU spike on api-server', got %q", result.Headline)
	}
}

func TestGetInvestigation_NotFound(t *testing.T) {
	t.Parallel()
	srv := newTestServer(
		&mockInvestigationReader{err: contracts.ErrNotFound},
		&mockEvidenceReader{},
		&mockTimelineReader{},
		&mockHypothesisReader{},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/missing", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "investigation not found" {
		t.Errorf("expected error 'investigation not found', got %q", body["error"])
	}
}

func TestListEvidence_Success(t *testing.T) {
	t.Parallel()
	evidence := []contracts.Evidence{
		{ID: "e1", InvestigationID: "inv-1", Kind: contracts.EvidenceLog, Source: "datadog"},
		{ID: "e2", InvestigationID: "inv-1", Kind: contracts.EvidenceMetric, Source: "prometheus"},
	}
	srv := newTestServer(
		&mockInvestigationReader{},
		&mockEvidenceReader{evidence: evidence},
		&mockTimelineReader{},
		&mockHypothesisReader{},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/inv-1/evidence", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var result []contracts.Evidence
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 evidence items, got %d", len(result))
	}
}

func TestListEvidence_Error(t *testing.T) {
	t.Parallel()
	srv := newTestServer(
		&mockInvestigationReader{},
		&mockEvidenceReader{err: errors.New("db error")},
		&mockTimelineReader{},
		&mockHypothesisReader{},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/inv-1/evidence", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

func TestListTimeline_Success(t *testing.T) {
	t.Parallel()
	events := []contracts.TimelineEvent{
		{
			ID:              "tl-1",
			InvestigationID: "inv-1",
			Kind:            contracts.TimelineAlert,
			Timestamp:       time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			Narrative:       "Alert fired",
		},
	}
	srv := newTestServer(
		&mockInvestigationReader{},
		&mockEvidenceReader{},
		&mockTimelineReader{events: events},
		&mockHypothesisReader{},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/inv-1/timeline", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var result []contracts.TimelineEvent
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 timeline event, got %d", len(result))
	}
	if result[0].Narrative != "Alert fired" {
		t.Errorf("expected narrative 'Alert fired', got %q", result[0].Narrative)
	}
}

func TestListTimeline_Error(t *testing.T) {
	t.Parallel()
	srv := newTestServer(
		&mockInvestigationReader{},
		&mockEvidenceReader{},
		&mockTimelineReader{err: errors.New("timeout")},
		&mockHypothesisReader{},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/inv-1/timeline", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

func TestListHypotheses_Success(t *testing.T) {
	t.Parallel()
	hypotheses := []contracts.Hypothesis{
		{
			ID:            "h-1",
			Title:         "Bad deploy",
			CauseCategory: contracts.CauseDeploy,
			Confidence:    0.85,
		},
	}
	srv := newTestServer(
		&mockInvestigationReader{},
		&mockEvidenceReader{},
		&mockTimelineReader{},
		&mockHypothesisReader{hypotheses: hypotheses},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/inv-1/hypotheses", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var result []contracts.Hypothesis
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 hypothesis, got %d", len(result))
	}
	if result[0].Title != "Bad deploy" {
		t.Errorf("expected title 'Bad deploy', got %q", result[0].Title)
	}
}

func TestListHypotheses_Error(t *testing.T) {
	t.Parallel()
	srv := newTestServer(
		&mockInvestigationReader{},
		&mockEvidenceReader{},
		&mockTimelineReader{},
		&mockHypothesisReader{err: errors.New("connection reset")},
	)
	router := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/inv-1/hypotheses", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

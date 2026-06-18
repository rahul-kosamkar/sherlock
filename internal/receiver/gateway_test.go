package receiver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

// --- mocks ---

type mockPublisher struct {
	mu        sync.Mutex
	published []struct {
		subject string
		data    []byte
	}
	err error
}

func (m *mockPublisher) Publish(_ context.Context, subject string, data []byte) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, struct {
		subject string
		data    []byte
	}{subject: subject, data: data})
	return nil
}

type mockBlobStore struct {
	mu     sync.Mutex
	stored []struct {
		key  string
		data []byte
	}
	err error
}

func (m *mockBlobStore) PutRawPayload(_ context.Context, key string, data []byte) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored = append(m.stored, struct {
		key  string
		data []byte
	}{key: key, data: data})
	return nil
}

type mockReceiver struct {
	source    string
	verifyErr error
	alerts    []contracts.NormalizedAlert
	decodeErr error
}

func (m *mockReceiver) Source() string { return m.source }

func (m *mockReceiver) Verify(_ context.Context, _ http.Header, _ []byte) error {
	return m.verifyErr
}

func (m *mockReceiver) Decode(_ context.Context, _ http.Header, _ []byte) ([]contracts.NormalizedAlert, error) {
	if m.decodeErr != nil {
		return nil, m.decodeErr
	}
	cp := make([]contracts.NormalizedAlert, len(m.alerts))
	copy(cp, m.alerts)
	return cp, nil
}

// --- helpers ---

func newTestGateway() (*Gateway, *mockPublisher, *mockBlobStore) {
	pub := &mockPublisher{}
	blob := &mockBlobStore{}
	gw := NewGateway(pub, blob, zap.NewNop())
	return gw, pub, blob
}

func doPost(t *testing.T, gw *Gateway, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gw.Routes().ServeHTTP(rec, req)
	return rec
}

// --- tests ---

func TestGateway_Routes_AcceptsPost(t *testing.T) {
	gw, _, _ := newTestGateway()
	gw.Register(&mockReceiver{
		source: "grafana",
		alerts: []contracts.NormalizedAlert{{Title: "cpu high"}},
	})

	rec := doPost(t, gw, "/webhooks/grafana", `{"status":"firing"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, rec.Code, rec.Body.String())
	}
}

func TestGateway_Routes_UnknownSource(t *testing.T) {
	gw, _, _ := newTestGateway()

	rec := doPost(t, gw, "/webhooks/unknown", `{}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestGateway_VerifyFailure(t *testing.T) {
	gw, _, _ := newTestGateway()
	gw.Register(&mockReceiver{
		source:    "grafana",
		verifyErr: errors.New("bad signature"),
	})

	rec := doPost(t, gw, "/webhooks/grafana", `{}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestGateway_DecodeFailure(t *testing.T) {
	gw, _, _ := newTestGateway()
	gw.Register(&mockReceiver{
		source:    "grafana",
		decodeErr: errors.New("malformed payload"),
	})

	rec := doPost(t, gw, "/webhooks/grafana", `{`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestGateway_PublishesAlerts(t *testing.T) {
	gw, pub, _ := newTestGateway()
	gw.Register(&mockReceiver{
		source: "pagerduty",
		alerts: []contracts.NormalizedAlert{
			{Title: "disk full"},
			{Title: "memory high"},
		},
	})

	rec := doPost(t, gw, "/webhooks/pagerduty", `{"event":"trigger"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, rec.Code, rec.Body.String())
	}
	if len(pub.published) != 2 {
		t.Fatalf("expected 2 published messages, got %d", len(pub.published))
	}
	for i, msg := range pub.published {
		if msg.subject != "INVESTIGATIONS.new" {
			t.Errorf("published[%d]: expected subject %q, got %q", i, "INVESTIGATIONS.new", msg.subject)
		}
		var job contracts.InvestigationJob
		if err := json.Unmarshal(msg.data, &job); err != nil {
			t.Fatalf("published[%d]: failed to unmarshal: %v", i, err)
		}
		if job.Alert.ID == "" {
			t.Errorf("published[%d]: expected non-empty alert ID", i)
		}
	}
}

func TestGateway_StoresRawPayload(t *testing.T) {
	gw, _, blob := newTestGateway()
	gw.Register(&mockReceiver{
		source: "grafana",
		alerts: []contracts.NormalizedAlert{{Title: "test"}},
	})

	payload := `{"alerts":[{"status":"firing"}]}`
	rec := doPost(t, gw, "/webhooks/grafana", payload)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, rec.Code, rec.Body.String())
	}
	if len(blob.stored) != 1 {
		t.Fatalf("expected 1 stored blob, got %d", len(blob.stored))
	}
	if !strings.HasPrefix(blob.stored[0].key, "raw/grafana/") {
		t.Errorf("expected blob key to start with %q, got %q", "raw/grafana/", blob.stored[0].key)
	}
	if string(blob.stored[0].data) != payload {
		t.Errorf("stored data mismatch: got %q", string(blob.stored[0].data))
	}
}

func TestGateway_PublisherError(t *testing.T) {
	gw, pub, _ := newTestGateway()
	pub.err = errors.New("nats unavailable")
	gw.Register(&mockReceiver{
		source: "grafana",
		alerts: []contracts.NormalizedAlert{{Title: "test"}},
	})

	rec := doPost(t, gw, "/webhooks/grafana", `{}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d: %s", http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
}

func TestGateway_BlobStoreError(t *testing.T) {
	gw, _, blob := newTestGateway()
	blob.err = errors.New("s3 unavailable")
	gw.Register(&mockReceiver{
		source: "grafana",
		alerts: []contracts.NormalizedAlert{{Title: "test"}},
	})

	rec := doPost(t, gw, "/webhooks/grafana", `{}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d: %s", http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
}

func TestGateway_Register(t *testing.T) {
	gw, _, _ := newTestGateway()

	sources := []string{"grafana", "pagerduty", "cloudwatch"}
	for _, src := range sources {
		gw.Register(&mockReceiver{
			source: src,
			alerts: []contracts.NormalizedAlert{{Title: src + " alert"}},
		})
	}

	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			rec := doPost(t, gw, "/webhooks/"+src, `{}`)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("expected status %d for source %q, got %d: %s",
					http.StatusAccepted, src, rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("unregistered", func(t *testing.T) {
		t.Parallel()
		rec := doPost(t, gw, "/webhooks/datadog", `{}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
		}
	})
}

func TestGateway_TraceContextInjection(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer otel.SetTextMapPropagator(prev)

	gw, pub, _ := newTestGateway()
	gw.Register(&mockReceiver{
		source: "grafana",
		alerts: []contracts.NormalizedAlert{{Title: "traced alert"}},
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/grafana", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("Tracestate", "rojo=00f067aa0ba902b7")

	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	gw.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, rec.Code, rec.Body.String())
	}
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}

	var job contracts.InvestigationJob
	if err := json.Unmarshal(pub.published[0].data, &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.TraceParent == "" {
		t.Error("expected non-empty TraceParent in published job")
	}
	if job.TraceState == "" {
		t.Error("expected non-empty TraceState in published job")
	}
}

func TestGateway_ConcurrentWebhooks(t *testing.T) {
	gw, pub, _ := newTestGateway()
	gw.Register(&mockReceiver{
		source: "grafana",
		alerts: []contracts.NormalizedAlert{{Title: "concurrent alert"}},
	})

	const n = 10
	errs := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() {
			r := doPost(t, gw, "/webhooks/grafana", `{}`)
			errs <- r.Code
		}()
	}

	for i := 0; i < n; i++ {
		code := <-errs
		if code != http.StatusAccepted {
			t.Errorf("request %d: expected %d, got %d", i, http.StatusAccepted, code)
		}
	}

	if len(pub.published) != n {
		t.Errorf("expected %d published messages, got %d", n, len(pub.published))
	}
}

func TestGateway_BodySizeLimit(t *testing.T) {
	gw, _, _ := newTestGateway()
	gw.Register(&mockReceiver{
		source:    "grafana",
		decodeErr: errors.New("should not reach decode"),
	})

	bigBody := strings.Repeat("x", 2<<20)
	rec := doPost(t, gw, "/webhooks/grafana", bigBody)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d: %s", http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	}
}

func TestGateway_AlertFieldsPopulated(t *testing.T) {
	gw, pub, blob := newTestGateway()
	gw.Register(&mockReceiver{
		source: "grafana",
		alerts: []contracts.NormalizedAlert{{Title: "latency spike", Source: "grafana"}},
	})

	doPost(t, gw, "/webhooks/grafana", `{}`)

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}

	var job contracts.InvestigationJob
	if err := json.Unmarshal(pub.published[0].data, &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if job.Alert.ID == "" {
		t.Error("expected non-empty alert ID")
	}
	if job.Alert.RawRef == "" {
		t.Error("expected non-empty RawRef")
	}
	if !strings.HasPrefix(job.Alert.RawRef, "raw/grafana/") {
		t.Errorf("expected RawRef to start with %q, got %q", "raw/grafana/", job.Alert.RawRef)
	}
	if len(blob.stored) != 1 {
		t.Fatalf("expected 1 stored blob, got %d", len(blob.stored))
	}
	if job.Alert.RawRef != blob.stored[0].key {
		t.Errorf("RawRef %q does not match blob key %q", job.Alert.RawRef, blob.stored[0].key)
	}
}

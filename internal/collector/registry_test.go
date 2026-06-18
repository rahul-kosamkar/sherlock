package collector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type mockCollector struct {
	name     string
	evidence []contracts.Evidence
	err      error
	delay    time.Duration
	called   atomic.Int32
	lastReq  contracts.CollectRequest
}

func (m *mockCollector) Name() string { return m.name }

func (m *mockCollector) Collect(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	m.called.Add(1)
	m.lastReq = req
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.evidence, m.err
}

func TestRegistry_Register(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(zap.NewNop())

	c1 := &mockCollector{name: "c1", evidence: []contracts.Evidence{{ID: "e1"}}}
	c2 := &mockCollector{name: "c2", evidence: []contracts.Evidence{{ID: "e2"}}}

	reg.Register(c1)
	reg.Register(c2)

	_, err := reg.CollectAll(context.Background(), contracts.CollectRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c1.called.Load() != 1 {
		t.Errorf("expected c1 to be called once, got %d", c1.called.Load())
	}
	if c2.called.Load() != 1 {
		t.Errorf("expected c2 to be called once, got %d", c2.called.Load())
	}
}

func TestRegistry_CollectAll_AggregatesResults(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(zap.NewNop())

	reg.Register(&mockCollector{
		name: "logs",
		evidence: []contracts.Evidence{
			{ID: "e1", Source: "logs"},
			{ID: "e2", Source: "logs"},
		},
	})
	reg.Register(&mockCollector{
		name: "metrics",
		evidence: []contracts.Evidence{
			{ID: "e3", Source: "metrics"},
			{ID: "e4", Source: "metrics"},
		},
	})

	results, err := reg.CollectAll(context.Background(), contracts.CollectRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("expected 4 evidence items, got %d", len(results))
	}
}

func TestRegistry_CollectAll_CollectorError_ContinuesOthers(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(zap.NewNop())

	reg.Register(&mockCollector{
		name: "failing",
		err:  errors.New("connection refused"),
	})
	reg.Register(&mockCollector{
		name: "healthy",
		evidence: []contracts.Evidence{
			{ID: "e1", Source: "healthy"},
		},
	})

	results, err := reg.CollectAll(context.Background(), contracts.CollectRequest{})
	if err != nil {
		t.Fatalf("expected nil error when one collector fails, got: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 evidence item from healthy collector, got %d", len(results))
	}
	if results[0].ID != "e1" {
		t.Errorf("expected evidence ID 'e1', got %q", results[0].ID)
	}
}

func TestRegistry_CollectAll_NoCollectors(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(zap.NewNop())

	results, err := reg.CollectAll(context.Background(), contracts.CollectRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRegistry_CollectAll_Parallel(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(zap.NewNop())

	reg.Register(&mockCollector{
		name:     "slow-a",
		delay:    100 * time.Millisecond,
		evidence: []contracts.Evidence{{ID: "a"}},
	})
	reg.Register(&mockCollector{
		name:     "slow-b",
		delay:    100 * time.Millisecond,
		evidence: []contracts.Evidence{{ID: "b"}},
	})

	start := time.Now()
	results, err := reg.CollectAll(context.Background(), contracts.CollectRequest{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if elapsed >= 250*time.Millisecond {
		t.Errorf("expected parallel execution under 250ms, took %v", elapsed)
	}
}

func TestRegistry_CollectAll_ContextCancellation(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(zap.NewNop())

	reg.Register(&mockCollector{
		name:     "will-cancel",
		delay:    500 * time.Millisecond,
		evidence: []contracts.Evidence{{ID: "e1"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not panic regardless of outcome.
	_, _ = reg.CollectAll(ctx, contracts.CollectRequest{})
}

func TestRegistry_CollectAll_PassesRequest(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(zap.NewNop())

	c1 := &mockCollector{name: "c1", evidence: []contracts.Evidence{{ID: "e1"}}}
	c2 := &mockCollector{name: "c2", evidence: []contracts.Evidence{{ID: "e2"}}}
	reg.Register(c1)
	reg.Register(c2)

	req := contracts.CollectRequest{
		InvestigationID: "inv-123",
		Targets: []contracts.TargetRef{
			{Kind: "service", Name: "api"},
		},
		TimeFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeTo:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	_, err := reg.CollectAll(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, c := range []*mockCollector{c1, c2} {
		if c.lastReq.InvestigationID != req.InvestigationID {
			t.Errorf("collector %s: expected InvestigationID %q, got %q",
				c.name, req.InvestigationID, c.lastReq.InvestigationID)
		}
		if len(c.lastReq.Targets) != 1 || c.lastReq.Targets[0].Name != "api" {
			t.Errorf("collector %s: expected Targets to contain 'api', got %v",
				c.name, c.lastReq.Targets)
		}
		if !c.lastReq.TimeFrom.Equal(req.TimeFrom) {
			t.Errorf("collector %s: expected TimeFrom %v, got %v",
				c.name, req.TimeFrom, c.lastReq.TimeFrom)
		}
		if !c.lastReq.TimeTo.Equal(req.TimeTo) {
			t.Errorf("collector %s: expected TimeTo %v, got %v",
				c.name, req.TimeTo, c.lastReq.TimeTo)
		}
	}
}

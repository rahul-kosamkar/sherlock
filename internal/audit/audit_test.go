package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type mockAuditStore struct {
	entries []*contracts.AuditEntry
	err     error
}

func (m *mockAuditStore) Create(_ context.Context, entry *contracts.AuditEntry) error {
	m.entries = append(m.entries, entry)
	return m.err
}

func TestLogger_Log_CreatesEntry(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{}
	logger := NewLogger(store, zap.NewNop())

	meta := map[string]string{"key": "value"}
	err := logger.Log(context.Background(), "tenant-1", "user@example.com", "create", "investigation/inv-1", meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(store.entries))
	}

	entry := store.entries[0]
	if entry.ID == "" {
		t.Error("expected non-empty ID")
	}
	if entry.TenantID != "tenant-1" {
		t.Errorf("expected TenantID 'tenant-1', got %q", entry.TenantID)
	}
	if entry.Actor != "user@example.com" {
		t.Errorf("expected Actor 'user@example.com', got %q", entry.Actor)
	}
	if entry.Action != "create" {
		t.Errorf("expected Action 'create', got %q", entry.Action)
	}
	if entry.Target != "investigation/inv-1" {
		t.Errorf("expected Target 'investigation/inv-1', got %q", entry.Target)
	}
	if entry.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
	if entry.Metadata["key"] != "value" {
		t.Errorf("expected metadata key=value, got %v", entry.Metadata)
	}
}

func TestLogger_Log_StoreError(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{err: errors.New("db unreachable")}
	logger := NewLogger(store, zap.NewNop())

	err := logger.Log(context.Background(), "t", "a", "act", "tgt", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLogger_LogAction_FillsID(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{}
	logger := NewLogger(store, zap.NewNop())

	entry := &contracts.AuditEntry{
		TenantID:  "t",
		Actor:     "a",
		Action:    "x",
		Timestamp: time.Now(),
	}

	err := logger.LogAction(context.Background(), entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.ID == "" {
		t.Error("expected ID to be filled")
	}
}

func TestLogger_LogAction_FillsTimestamp(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{}
	logger := NewLogger(store, zap.NewNop())

	entry := &contracts.AuditEntry{
		ID:     "existing-id",
		Actor:  "a",
		Action: "x",
	}

	err := logger.LogAction(context.Background(), entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Timestamp.IsZero() {
		t.Error("expected Timestamp to be filled")
	}
}

func TestLogger_LogAction_PreservesExistingID(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{}
	logger := NewLogger(store, zap.NewNop())

	entry := &contracts.AuditEntry{
		ID:        "my-custom-id",
		Actor:     "a",
		Action:    "x",
		Timestamp: time.Now(),
	}

	err := logger.LogAction(context.Background(), entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.ID != "my-custom-id" {
		t.Errorf("expected ID 'my-custom-id', got %q", entry.ID)
	}
}

func TestLogger_LogAction_PreservesExistingTimestamp(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{}
	logger := NewLogger(store, zap.NewNop())

	fixedTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	entry := &contracts.AuditEntry{
		ID:        "id",
		Actor:     "a",
		Action:    "x",
		Timestamp: fixedTime,
	}

	err := logger.LogAction(context.Background(), entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !entry.Timestamp.Equal(fixedTime) {
		t.Errorf("expected timestamp %v, got %v", fixedTime, entry.Timestamp)
	}
}

func TestLogger_LogAction_StoreError(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{err: errors.New("disk full")}
	logger := NewLogger(store, zap.NewNop())

	entry := &contracts.AuditEntry{
		ID:        "id",
		Actor:     "a",
		Action:    "x",
		Timestamp: time.Now(),
	}

	err := logger.LogAction(context.Background(), entry)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, store.err) {
		t.Errorf("expected wrapped error containing %v, got %v", store.err, err)
	}
}

func TestLogger_Log_MetadataPassed(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{}
	logger := NewLogger(store, zap.NewNop())

	meta := map[string]string{
		"investigation_id": "inv-42",
		"severity":         "critical",
	}

	err := logger.Log(context.Background(), "t", "a", "act", "tgt", meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := store.entries[0]
	if len(entry.Metadata) != 2 {
		t.Fatalf("expected 2 metadata entries, got %d", len(entry.Metadata))
	}
	if entry.Metadata["investigation_id"] != "inv-42" {
		t.Errorf("expected investigation_id=inv-42, got %q", entry.Metadata["investigation_id"])
	}
	if entry.Metadata["severity"] != "critical" {
		t.Errorf("expected severity=critical, got %q", entry.Metadata["severity"])
	}
}

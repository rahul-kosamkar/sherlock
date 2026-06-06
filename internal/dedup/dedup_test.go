package dedup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type mockLookup struct {
	findResult *ActiveInvestigation
	findErr    error
	linkCalled bool
	linkErr    error
}

func (m *mockLookup) FindActiveByFingerprint(_ context.Context, _ string, _ time.Time) (*ActiveInvestigation, error) {
	return m.findResult, m.findErr
}

func (m *mockLookup) LinkAlertToInvestigation(_ context.Context, _, _ string) error {
	m.linkCalled = true
	return m.linkErr
}

func TestNew_CreatesServiceWithCorrectWindow(t *testing.T) {
	window := 30 * time.Minute
	svc := New(&mockLookup{}, window, zap.NewNop())
	if svc == nil {
		t.Fatal("New() returned nil")
	}
	if svc.window != window {
		t.Errorf("window = %v, want %v", svc.window, window)
	}
}

func TestCheck_EmptyFingerprint_NotDuplicate(t *testing.T) {
	svc := New(&mockLookup{}, time.Hour, zap.NewNop())
	alert := contracts.NormalizedAlert{
		ID:          "alert-1",
		Fingerprint: "",
	}

	result, err := svc.Check(context.Background(), alert)
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.IsDuplicate {
		t.Error("expected IsDuplicate=false for empty fingerprint")
	}
}

func TestCheck_NoActiveInvestigation_NotDuplicate(t *testing.T) {
	mock := &mockLookup{findResult: nil, findErr: nil}
	svc := New(mock, time.Hour, zap.NewNop())
	alert := contracts.NormalizedAlert{
		ID:          "alert-1",
		Fingerprint: "fp-abc123",
	}

	result, err := svc.Check(context.Background(), alert)
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.IsDuplicate {
		t.Error("expected IsDuplicate=false when no active investigation found")
	}
}

func TestCheck_ActiveInvestigationFound_IsDuplicate(t *testing.T) {
	existing := &ActiveInvestigation{
		ID:             "inv-1",
		Status:         "collecting",
		Headline:       "Payment failures",
		SlackChannelID: "C123",
		SlackThreadTS:  "1234567890.123456",
	}
	mock := &mockLookup{findResult: existing, findErr: nil}
	svc := New(mock, time.Hour, zap.NewNop())
	alert := contracts.NormalizedAlert{
		ID:          "alert-2",
		Fingerprint: "fp-abc123",
	}

	result, err := svc.Check(context.Background(), alert)
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if !result.IsDuplicate {
		t.Fatal("expected IsDuplicate=true when active investigation found")
	}
	if result.ExistingID != "inv-1" {
		t.Errorf("ExistingID = %q, want %q", result.ExistingID, "inv-1")
	}
	if result.ExistingHeadline != "Payment failures" {
		t.Errorf("ExistingHeadline = %q, want %q", result.ExistingHeadline, "Payment failures")
	}
	if result.ExistingChannel != "C123" {
		t.Errorf("ExistingChannel = %q, want %q", result.ExistingChannel, "C123")
	}
	if result.ExistingThread != "1234567890.123456" {
		t.Errorf("ExistingThread = %q, want %q", result.ExistingThread, "1234567890.123456")
	}
	if !mock.linkCalled {
		t.Error("expected LinkAlertToInvestigation to be called")
	}
}

func TestCheck_StoreError_ReturnsError(t *testing.T) {
	mock := &mockLookup{findResult: nil, findErr: fmt.Errorf("database connection failed")}
	svc := New(mock, time.Hour, zap.NewNop())
	alert := contracts.NormalizedAlert{
		ID:          "alert-1",
		Fingerprint: "fp-abc123",
	}

	_, err := svc.Check(context.Background(), alert)
	if err == nil {
		t.Fatal("Check() should return error when store fails")
	}
}

func TestCheck_LinkError_StillReturnsDuplicate(t *testing.T) {
	existing := &ActiveInvestigation{
		ID:             "inv-1",
		Status:         "collecting",
		Headline:       "Test",
		SlackChannelID: "C123",
		SlackThreadTS:  "123.456",
	}
	mock := &mockLookup{
		findResult: existing,
		findErr:    nil,
		linkErr:    fmt.Errorf("link failed"),
	}
	svc := New(mock, time.Hour, zap.NewNop())
	alert := contracts.NormalizedAlert{
		ID:          "alert-3",
		Fingerprint: "fp-def456",
	}

	result, err := svc.Check(context.Background(), alert)
	if err != nil {
		t.Fatalf("Check() should not return error on link failure, got: %v", err)
	}
	if !result.IsDuplicate {
		t.Error("expected IsDuplicate=true even when link fails")
	}
}

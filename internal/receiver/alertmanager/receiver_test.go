package alertmanager_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/receiver/alertmanager"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "alertmanager", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

func TestDecode_FiringAlert(t *testing.T) {
	t.Parallel()
	body := loadFixture(t, "alert_firing.json")
	r := alertmanager.New("")

	alerts, err := r.Decode(context.Background(), nil, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Source != "alertmanager" {
		t.Errorf("Source = %q, want %q", a.Source, "alertmanager")
	}
	if a.Status != contracts.AlertStatusFiring {
		t.Errorf("Status = %q, want %q", a.Status, contracts.AlertStatusFiring)
	}
	if a.Severity != contracts.SeverityCritical {
		t.Errorf("Severity = %q, want %q", a.Severity, contracts.SeverityCritical)
	}
	if a.Title != "High error rate on checkout-service" {
		t.Errorf("Title = %q, unexpected", a.Title)
	}
	if a.Fingerprint != "f7e6d5c4b3a20198" {
		t.Errorf("Fingerprint = %q, want %q", a.Fingerprint, "f7e6d5c4b3a20198")
	}
	if a.Labels["service"] != "checkout-service" {
		t.Errorf("Labels[service] = %q, want %q", a.Labels["service"], "checkout-service")
	}
	if a.GroupKey != "{}:{alertname=\"HighErrorRate\"}" {
		t.Errorf("GroupKey = %q, unexpected", a.GroupKey)
	}
	if len(a.EntityHints) == 0 {
		t.Fatal("expected at least one EntityHint")
	}
	hint := a.EntityHints[0]
	if hint.Name != "checkout-service" || hint.Namespace != "prod" || hint.Cluster != "us-east-1" {
		t.Errorf("EntityHint = %+v, unexpected", hint)
	}
	if len(a.Links) < 2 {
		t.Fatalf("expected at least 2 links, got %d", len(a.Links))
	}
	if a.EndsAt != nil {
		t.Errorf("EndsAt should be nil for zero-value endsAt, got %v", a.EndsAt)
	}
}

func TestVerify_NoSecret(t *testing.T) {
	t.Parallel()
	r := alertmanager.New("")
	if err := r.Verify(context.Background(), nil, nil); err != nil {
		t.Fatalf("Verify() with empty secret should return nil, got: %v", err)
	}
}

func TestVerify_ValidSecret(t *testing.T) {
	t.Parallel()
	r := alertmanager.New("my-secret")
	h := make(map[string][]string)
	h["Authorization"] = []string{"Bearer my-secret"}
	if err := r.Verify(context.Background(), h, nil); err != nil {
		t.Fatalf("Verify() with valid secret should return nil, got: %v", err)
	}
}

func TestVerify_InvalidSecret(t *testing.T) {
	t.Parallel()
	r := alertmanager.New("my-secret")
	h := make(map[string][]string)
	h["Authorization"] = []string{"Bearer wrong-secret"}
	if err := r.Verify(context.Background(), h, nil); err == nil {
		t.Fatal("Verify() with invalid secret should return error")
	}
}

package grafana_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/receiver/grafana"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "grafana", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

func TestDecode_FiringAlert(t *testing.T) {
	t.Parallel()
	body := loadFixture(t, "alert_firing.json")
	r := grafana.New("")

	alerts, err := r.Decode(context.Background(), nil, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Source != "grafana" {
		t.Errorf("Source = %q, want %q", a.Source, "grafana")
	}
	if a.Status != contracts.AlertStatusFiring {
		t.Errorf("Status = %q, want %q", a.Status, contracts.AlertStatusFiring)
	}
	if a.Severity != contracts.SeverityCritical {
		t.Errorf("Severity = %q, want %q", a.Severity, contracts.SeverityCritical)
	}
	if a.Title != "[FIRING:1] HighCPUUsage (payments-api prod eu1 critical)" {
		t.Errorf("Title = %q, unexpected", a.Title)
	}
	if a.Fingerprint != "a1b2c3d4e5f60718" {
		t.Errorf("Fingerprint = %q, want %q", a.Fingerprint, "a1b2c3d4e5f60718")
	}
	if a.Labels["service"] != "payments-api" {
		t.Errorf("Labels[service] = %q, want %q", a.Labels["service"], "payments-api")
	}
	if len(a.EntityHints) == 0 {
		t.Fatal("expected at least one EntityHint")
	}
	hint := a.EntityHints[0]
	if hint.Name != "payments-api" || hint.Namespace != "prod" || hint.Cluster != "eu1" {
		t.Errorf("EntityHint = %+v, unexpected", hint)
	}
	if len(a.Links) < 2 {
		t.Fatalf("expected at least 2 links, got %d", len(a.Links))
	}
	if a.EndsAt != nil {
		t.Errorf("EndsAt should be nil for zero-value endsAt, got %v", a.EndsAt)
	}
}

func TestVerify_ValidSignature(t *testing.T) {
	t.Parallel()
	secret := "test-secret-key"
	body := []byte(`{"status":"firing"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	headers := http.Header{}
	headers.Set("X-Grafana-Signature", sig)

	r := grafana.New(secret)
	if err := r.Verify(context.Background(), headers, body); err != nil {
		t.Fatalf("Verify() returned error for valid signature: %v", err)
	}
}

func TestVerify_InvalidSignature(t *testing.T) {
	t.Parallel()
	secret := "test-secret-key"
	body := []byte(`{"status":"firing"}`)

	headers := http.Header{}
	headers.Set("X-Grafana-Signature", "deadbeef")

	r := grafana.New(secret)
	if err := r.Verify(context.Background(), headers, body); err == nil {
		t.Fatal("Verify() should return error for invalid signature")
	}
}

func TestVerify_NoSecret_SkipsValidation(t *testing.T) {
	t.Parallel()
	r := grafana.New("")
	if err := r.Verify(context.Background(), http.Header{}, nil); err != nil {
		t.Fatalf("Verify() should pass when no secret is configured, got: %v", err)
	}
}

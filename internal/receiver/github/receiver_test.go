package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func TestSource(t *testing.T) {
	t.Parallel()
	r := New("secret")
	if got := r.Source(); got != "github" {
		t.Errorf("Source() = %q, want %q", got, "github")
	}
}

func TestVerify_ValidSignature(t *testing.T) {
	t.Parallel()
	secret := "my-webhook-secret"
	body := []byte(`{"action":"created"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", sig)

	r := New(secret)
	if err := r.Verify(context.Background(), headers, body); err != nil {
		t.Fatalf("Verify() returned error for valid signature: %v", err)
	}
}

func TestVerify_InvalidSignature(t *testing.T) {
	t.Parallel()
	secret := "my-webhook-secret"
	body := []byte(`{"action":"created"}`)

	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "sha256=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	r := New(secret)
	if err := r.Verify(context.Background(), headers, body); err == nil {
		t.Fatal("Verify() should return error for invalid signature")
	}
}

func TestVerify_MissingHeader(t *testing.T) {
	t.Parallel()
	r := New("secret")
	err := r.Verify(context.Background(), http.Header{}, []byte("body"))
	if err == nil {
		t.Fatal("Verify() should return error when header is missing")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %q, want mention of missing header", err.Error())
	}
}

func TestVerify_MalformedPrefix(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "md5=abc123")

	r := New("secret")
	err := r.Verify(context.Background(), headers, []byte("body"))
	if err == nil {
		t.Fatal("Verify() should return error for malformed prefix")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("error = %q, want mention of malformed", err.Error())
	}
}

func TestVerify_EmptySecret_SkipsValidation(t *testing.T) {
	t.Parallel()
	r := New("")
	if err := r.Verify(context.Background(), http.Header{}, nil); err != nil {
		t.Fatalf("Verify() should pass when no secret is configured, got: %v", err)
	}
}

func TestDecode_DeploymentEvent(t *testing.T) {
	t.Parallel()
	payload := deploymentEvent{
		Action: "created",
		Deployment: deployment{
			ID:          1,
			SHA:         "abc123def456",
			Ref:         "main",
			Environment: "production",
			Description: "Deploy v1.2.3",
			URL:         "https://api.github.com/repos/org/repo/deployments/1",
			Creator:     sender{Login: "deployer"},
		},
		Repository: repository{
			FullName: "org/repo",
			Name:     "repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		Sender: sender{Login: "deployer"},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "deployment")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Source != "github" {
		t.Errorf("Source = %q, want %q", a.Source, "github")
	}
	if a.Status != contracts.AlertStatusFiring {
		t.Errorf("Status = %q, want %q", a.Status, contracts.AlertStatusFiring)
	}
	if a.Severity != contracts.SeverityInfo {
		t.Errorf("Severity = %q, want %q", a.Severity, contracts.SeverityInfo)
	}
	if !strings.Contains(a.Title, "production") {
		t.Errorf("Title = %q, should contain environment", a.Title)
	}
	if !strings.Contains(a.Title, "org/repo") {
		t.Errorf("Title = %q, should contain repo name", a.Title)
	}
	if a.Summary != "Deploy v1.2.3" {
		t.Errorf("Summary = %q, want %q", a.Summary, "Deploy v1.2.3")
	}
	if a.Labels["repo"] != "org/repo" {
		t.Errorf("Labels[repo] = %q, want %q", a.Labels["repo"], "org/repo")
	}
	if a.Labels["environment"] != "production" {
		t.Errorf("Labels[environment] = %q, want %q", a.Labels["environment"], "production")
	}
	if a.Labels["sha"] != "abc123def456" {
		t.Errorf("Labels[sha] = %q, want %q", a.Labels["sha"], "abc123def456")
	}
	if a.Fingerprint == "" {
		t.Error("Fingerprint should not be empty")
	}
	if len(a.EntityHints) == 0 {
		t.Fatal("expected at least one EntityHint")
	}
	if a.EntityHints[0].Kind != "repo" {
		t.Errorf("EntityHints[0].Kind = %q, want %q", a.EntityHints[0].Kind, "repo")
	}
	if len(a.Links) < 1 {
		t.Errorf("expected at least 1 link, got %d", len(a.Links))
	}
}

func TestDecode_PushEvent(t *testing.T) {
	t.Parallel()
	payload := pushEvent{
		Ref:     "refs/heads/main",
		Before:  "0000000000000000000000000000000000000000",
		After:   "abc123def456789",
		Compare: "https://github.com/org/repo/compare/000000...abc123",
		HeadCommit: &commit{
			ID:      "abc123def456789",
			Message: "fix: resolve timeout issue",
			URL:     "https://github.com/org/repo/commit/abc123",
		},
		Commits: []commit{
			{ID: "abc123def456789", Message: "fix: resolve timeout issue"},
		},
		Repository: repository{
			FullName: "org/repo",
			Name:     "repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		Sender: sender{Login: "developer"},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Source != "github" {
		t.Errorf("Source = %q, want %q", a.Source, "github")
	}
	if !strings.Contains(a.Title, "refs/heads/main") {
		t.Errorf("Title = %q, should contain ref", a.Title)
	}
	if a.Summary != "fix: resolve timeout issue" {
		t.Errorf("Summary = %q, want head commit message", a.Summary)
	}
	if a.Labels["ref"] != "refs/heads/main" {
		t.Errorf("Labels[ref] = %q, want %q", a.Labels["ref"], "refs/heads/main")
	}
	if a.Labels["head_commit"] != "abc123def456789" {
		t.Errorf("Labels[head_commit] = %q, want %q", a.Labels["head_commit"], "abc123def456789")
	}
	if len(a.Links) == 0 {
		t.Fatal("expected compare link")
	}
	if a.Links[0].Rel != "compare" {
		t.Errorf("Links[0].Rel = %q, want %q", a.Links[0].Rel, "compare")
	}
}

func TestDecode_DeploymentStatusEvent_Failure(t *testing.T) {
	t.Parallel()
	payload := deploymentStatusEvent{
		DeploymentStatus: deploymentState{
			ID:          100,
			State:       "failure",
			Description: "Deployment timed out",
			TargetURL:   "https://ci.example.com/builds/100",
		},
		Deployment: deployment{
			ID:          1,
			SHA:         "deadbeef123",
			Ref:         "main",
			Environment: "staging",
			URL:         "https://api.github.com/repos/org/repo/deployments/1",
		},
		Repository: repository{
			FullName: "org/repo",
			Name:     "repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		Sender: sender{Login: "ci-bot"},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "deployment_status")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert for failure status, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Severity != contracts.SeverityWarning {
		t.Errorf("Severity = %q, want %q", a.Severity, contracts.SeverityWarning)
	}
	if !strings.Contains(a.Title, "failure") {
		t.Errorf("Title = %q, should contain 'failure'", a.Title)
	}
	if a.Labels["status"] != "failure" {
		t.Errorf("Labels[status] = %q, want %q", a.Labels["status"], "failure")
	}
	if a.Labels["environment"] != "staging" {
		t.Errorf("Labels[environment] = %q, want %q", a.Labels["environment"], "staging")
	}
}

func TestDecode_DeploymentStatusEvent_Success_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	payload := deploymentStatusEvent{
		DeploymentStatus: deploymentState{
			ID:    100,
			State: "success",
		},
		Deployment: deployment{
			SHA:         "abc123",
			Environment: "production",
		},
		Repository: repository{FullName: "org/repo", Name: "repo"},
		Sender:     sender{Login: "ci-bot"},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "deployment_status")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if alerts != nil {
		t.Errorf("expected nil alerts for success status, got %d", len(alerts))
	}
}

func TestDecode_UnknownEvent_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "issues")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, []byte(`{}`))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if alerts != nil {
		t.Errorf("expected nil for unknown event type, got %d alerts", len(alerts))
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	t.Parallel()
	fp1 := fingerprint("org/repo", "production", "abc123")
	fp2 := fingerprint("org/repo", "production", "abc123")
	if fp1 != fp2 {
		t.Errorf("fingerprint not deterministic: %q != %q", fp1, fp2)
	}

	fp3 := fingerprint("org/repo", "staging", "abc123")
	if fp1 == fp3 {
		t.Error("fingerprints should differ for different inputs")
	}
}

func TestFingerprint_Format(t *testing.T) {
	t.Parallel()
	fp := fingerprint("org/repo", "production", "abc123")
	if len(fp) != 64 {
		t.Errorf("fingerprint length = %d, want 64 (sha256 hex)", len(fp))
	}
	for _, c := range fp {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Errorf("fingerprint contains non-hex char: %c", c)
			break
		}
	}
}

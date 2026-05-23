package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func TestSource(t *testing.T) {
	r := New("token")
	if got := r.Source(); got != "gitlab" {
		t.Errorf("Source() = %q, want %q", got, "gitlab")
	}
}

func TestVerify_ValidToken(t *testing.T) {
	secret := "my-gitlab-token"
	headers := http.Header{}
	headers.Set("X-Gitlab-Token", secret)

	r := New(secret)
	if err := r.Verify(context.Background(), headers, nil); err != nil {
		t.Fatalf("Verify() returned error for valid token: %v", err)
	}
}

func TestVerify_InvalidToken(t *testing.T) {
	secret := "my-gitlab-token"
	headers := http.Header{}
	headers.Set("X-Gitlab-Token", "wrong-token")

	r := New(secret)
	if err := r.Verify(context.Background(), headers, nil); err == nil {
		t.Fatal("Verify() should return error for invalid token")
	}
}

func TestVerify_MissingHeader(t *testing.T) {
	r := New("my-secret")
	err := r.Verify(context.Background(), http.Header{}, nil)
	if err == nil {
		t.Fatal("Verify() should return error when header is missing")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %q, want mention of missing header", err.Error())
	}
}

func TestVerify_EmptySecret_SkipsValidation(t *testing.T) {
	r := New("")
	if err := r.Verify(context.Background(), http.Header{}, nil); err != nil {
		t.Fatalf("Verify() should pass when no secret is configured, got: %v", err)
	}
}

func TestDecode_DeploymentHook_Running(t *testing.T) {
	payload := deploymentHookEvent{
		ObjectKind:   "deployment",
		Status:       "running",
		DeployableID: 42,
		Environment:  "production",
		ShortSHA:     "abc1234",
		CommitURL:    "https://gitlab.com/org/repo/-/commit/abc1234",
		CommitTitle:  "feat: add new feature",
		User:         "Alice",
		UserUsername: "alice",
		Project: project{
			Name:              "repo",
			PathWithNamespace: "org/repo",
			WebURL:            "https://gitlab.com/org/repo",
		},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-Gitlab-Event", "Deployment Hook")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Source != "gitlab" {
		t.Errorf("Source = %q, want %q", a.Source, "gitlab")
	}
	if a.Status != contracts.AlertStatusFiring {
		t.Errorf("Status = %q, want %q", a.Status, contracts.AlertStatusFiring)
	}
	if a.Severity != contracts.SeverityInfo {
		t.Errorf("Severity = %q, want %q for running status", a.Severity, contracts.SeverityInfo)
	}
	if !strings.Contains(a.Title, "running") {
		t.Errorf("Title = %q, should contain status", a.Title)
	}
	if !strings.Contains(a.Title, "repo") {
		t.Errorf("Title = %q, should contain project name", a.Title)
	}
	if a.Labels["project"] != "org/repo" {
		t.Errorf("Labels[project] = %q, want %q", a.Labels["project"], "org/repo")
	}
	if a.Labels["environment"] != "production" {
		t.Errorf("Labels[environment] = %q, want %q", a.Labels["environment"], "production")
	}
	if a.Labels["sha"] != "abc1234" {
		t.Errorf("Labels[sha] = %q, want %q", a.Labels["sha"], "abc1234")
	}
	if a.Labels["user"] != "alice" {
		t.Errorf("Labels[user] = %q, want %q", a.Labels["user"], "alice")
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
	if a.EntityHints[0].Environment != "production" {
		t.Errorf("EntityHints[0].Environment = %q, want %q", a.EntityHints[0].Environment, "production")
	}
}

func TestDecode_DeploymentHook_Success_ReturnsEmpty(t *testing.T) {
	payload := deploymentHookEvent{
		ObjectKind:   "deployment",
		Status:       "success",
		Environment:  "production",
		ShortSHA:     "abc1234",
		UserUsername: "alice",
		Project: project{
			Name:              "repo",
			PathWithNamespace: "org/repo",
		},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-Gitlab-Event", "Deployment Hook")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if alerts != nil {
		t.Errorf("expected nil alerts for success status, got %d", len(alerts))
	}
}

func TestDecode_DeploymentHook_Failed(t *testing.T) {
	payload := deploymentHookEvent{
		ObjectKind:   "deployment",
		Status:       "failed",
		Environment:  "staging",
		ShortSHA:     "def5678",
		UserUsername: "bob",
		Project: project{
			Name:              "service",
			PathWithNamespace: "team/service",
			WebURL:            "https://gitlab.com/team/service",
		},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-Gitlab-Event", "Deployment Hook")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert for failed status, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Severity != contracts.SeverityWarning {
		t.Errorf("Severity = %q, want %q for failed status", a.Severity, contracts.SeverityWarning)
	}
	if a.Labels["status"] != "failed" {
		t.Errorf("Labels[status] = %q, want %q", a.Labels["status"], "failed")
	}
}

func TestDecode_PushHook(t *testing.T) {
	payload := pushHookEvent{
		ObjectKind:   "push",
		Ref:          "refs/heads/main",
		Before:       "0000000000000000",
		After:        "abc123def456",
		UserName:     "Alice Developer",
		UserUsername: "alice",
		Commits: []commit{
			{ID: "abc123def456", Message: "fix: resolve memory leak", URL: "https://gitlab.com/org/repo/-/commit/abc123"},
		},
		Project: project{
			Name:              "repo",
			PathWithNamespace: "org/repo",
			WebURL:            "https://gitlab.com/org/repo",
		},
	}

	body, _ := json.Marshal(payload)
	headers := http.Header{}
	headers.Set("X-Gitlab-Event", "Push Hook")

	r := New("")
	alerts, err := r.Decode(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Source != "gitlab" {
		t.Errorf("Source = %q, want %q", a.Source, "gitlab")
	}
	if !strings.Contains(a.Title, "refs/heads/main") {
		t.Errorf("Title = %q, should contain ref", a.Title)
	}
	if a.Summary != "fix: resolve memory leak" {
		t.Errorf("Summary = %q, want last commit message", a.Summary)
	}
	if a.Labels["project"] != "org/repo" {
		t.Errorf("Labels[project] = %q, want %q", a.Labels["project"], "org/repo")
	}
	if a.Labels["ref"] != "refs/heads/main" {
		t.Errorf("Labels[ref] = %q, want %q", a.Labels["ref"], "refs/heads/main")
	}
	if a.Labels["sha"] != "abc123def456" {
		t.Errorf("Labels[sha] = %q, want %q", a.Labels["sha"], "abc123def456")
	}
	if len(a.Links) == 0 {
		t.Fatal("expected project link")
	}
	if a.Links[0].Rel != "project" {
		t.Errorf("Links[0].Rel = %q, want %q", a.Links[0].Rel, "project")
	}
}

func TestDecode_UnknownEvent_ReturnsEmpty(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Gitlab-Event", "Merge Request Hook")

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

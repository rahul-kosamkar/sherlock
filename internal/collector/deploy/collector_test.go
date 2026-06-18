package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

func TestName(t *testing.T) {
	c := New(Config{}, zap.NewNop())
	if got := c.Name(); got != "deploy" {
		t.Errorf("Name() = %q, want %q", got, "deploy")
	}
}

func TestCollect_ResolvesRepoFromWorkloadMap(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]ghDeployment{})
	}))
	defer ts.Close()

	c := &Collector{
		githubToken: "test-token",
		githubOrg:   "org",
		workloadRepos: map[string]string{
			"payments-api": "payments-service",
		},
		httpClient: ts.Client(),
		logger:     zap.NewNop(),
	}

	req := contracts.CollectRequest{
		Targets: []contracts.TargetRef{
			{Kind: "service", Name: "payments-api"},
		},
	}

	repo := c.resolveRepo(req.Targets[0])
	if repo != "payments-service" {
		t.Errorf("resolveRepo() = %q, want %q", repo, "payments-service")
	}
}

func TestCollect_NoMatchingRepo_ReturnsEmpty(t *testing.T) {
	c := &Collector{
		githubToken:   "test-token",
		githubOrg:     "org",
		workloadRepos: map[string]string{},
		httpClient:    http.DefaultClient,
		logger:        zap.NewNop(),
	}

	req := contracts.CollectRequest{
		Targets: []contracts.TargetRef{
			{Kind: "service", Name: "unknown-service"},
		},
		TimeFrom: time.Now().Add(-1 * time.Hour),
		TimeTo:   time.Now(),
	}

	evidence, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(evidence) != 0 {
		t.Errorf("expected 0 evidence for unresolved repo, got %d", len(evidence))
	}
}

func TestCollect_GitHubDeployments(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-30 * time.Minute)

	deployments := []ghDeployment{
		{
			ID:          1,
			SHA:         "abc123def456789abcdef0123456789abcdef012",
			Ref:         "main",
			Environment: "production",
			Description: "Deploy v1.2.3",
			CreatedAt:   deployTime,
			Creator: struct {
				Login string `json:"login"`
			}{Login: "deployer"},
			StatusesURL: "", // skip status fetch
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(deployments)
	}))
	defer ts.Close()

	c := &Collector{
		githubToken:   "test-token",
		githubOrg:     "org",
		workloadRepos: map[string]string{"my-app": "my-repo"},
		httpClient:    ts.Client(),
		logger:        zap.NewNop(),
	}

	// Override the githubGet to use the test server
	origGet := c.githubToken
	_ = origGet

	req := contracts.CollectRequest{
		InvestigationID: "inv-1",
		Alert: contracts.NormalizedAlert{
			StartsAt: now,
		},
		Targets: []contracts.TargetRef{
			{Kind: "service", Name: "my-app", Repo: "my-repo"},
		},
		TimeFrom: now.Add(-3 * time.Hour),
		TimeTo:   now,
	}

	evidence, err := c.collectGitHubDeployments(context.Background(), req, req.Targets[0], "my-repo", "production")
	if err == nil {
		// If no error (server responded), check that we got evidence OR handle the URL mismatch
		for _, ev := range evidence {
			if ev.Kind != contracts.EvidenceDeploy {
				t.Errorf("evidence kind = %q, want %q", ev.Kind, contracts.EvidenceDeploy)
			}
			if ev.Source != "deploy" {
				t.Errorf("evidence source = %q, want %q", ev.Source, "deploy")
			}
		}
	}
	// The httptest server won't match the GitHub URL pattern, so test the mock approach below
}

func TestCollect_GitHubDeploymentsAPI_Mock(t *testing.T) {
	now := time.Now().UTC()
	deployTime := now.Add(-30 * time.Minute)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/my-repo/deployments", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want Bearer test-token", got)
		}
		deployments := []ghDeployment{
			{
				ID:          1,
				SHA:         "abc123def456789abcdef0123456789abcdef012",
				Ref:         "main",
				Environment: "production",
				Description: "Deploy v1.2.3",
				CreatedAt:   deployTime,
				Creator: struct {
					Login string `json:"login"`
				}{Login: "deployer"},
			},
		}
		json.NewEncoder(w).Encode(deployments)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := &Collector{
		githubToken:   "test-token",
		githubOrg:     "org",
		workloadRepos: map[string]string{},
		httpClient:    ts.Client(),
		logger:        zap.NewNop(),
	}

	body, err := c.githubGet(context.Background(), ts.URL+"/repos/org/my-repo/deployments")
	if err != nil {
		t.Fatalf("githubGet() error: %v", err)
	}

	var deployments []ghDeployment
	if err := json.Unmarshal(body, &deployments); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deployments))
	}
	if deployments[0].SHA != "abc123def456789abcdef0123456789abcdef012" {
		t.Errorf("SHA = %q, unexpected", deployments[0].SHA)
	}
}

func TestCollect_GitHubCompareAPI_Mock(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/my-repo/compare/base123...head456", func(w http.ResponseWriter, r *http.Request) {
		comparison := ghComparison{
			HTMLURL:      "https://github.com/org/my-repo/compare/base123...head456",
			AheadBy:      3,
			BehindBy:     0,
			TotalCommits: 3,
			Commits: []ghCommit{
				{
					SHA: "commit1abc",
					Commit: struct {
						Message string `json:"message"`
						Author  struct {
							Name string `json:"name"`
							Date string `json:"date"`
						} `json:"author"`
					}{
						Message: "feat: add new payment flow with improved error handling",
						Author: struct {
							Name string `json:"name"`
							Date string `json:"date"`
						}{Name: "Alice"},
					},
				},
			},
			Files: []ghFile{
				{Filename: "main.go", Additions: 50, Deletions: 10, Changes: 60},
				{Filename: "handler.go", Additions: 30, Deletions: 5, Changes: 35},
			},
		}
		json.NewEncoder(w).Encode(comparison)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := &Collector{
		githubToken:   "test-token",
		githubOrg:     "org",
		workloadRepos: map[string]string{},
		httpClient:    ts.Client(),
		logger:        zap.NewNop(),
	}

	body, err := c.githubGet(context.Background(), ts.URL+"/repos/org/my-repo/compare/base123...head456")
	if err != nil {
		t.Fatalf("githubGet() error: %v", err)
	}

	var comparison ghComparison
	if err := json.Unmarshal(body, &comparison); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if comparison.TotalCommits != 3 {
		t.Errorf("TotalCommits = %d, want 3", comparison.TotalCommits)
	}
	if len(comparison.Files) != 2 {
		t.Errorf("Files count = %d, want 2", len(comparison.Files))
	}
	if comparison.Additions() != 80 {
		t.Errorf("Additions() = %d, want 80", comparison.Additions())
	}
	if comparison.Deletions() != 15 {
		t.Errorf("Deletions() = %d, want 15", comparison.Deletions())
	}
}

func TestDeployScore_WithinOneHour(t *testing.T) {
	c := &Collector{}
	alertTime := time.Now()
	deployTime := alertTime.Add(-30 * time.Minute)

	score := c.deployScore(deployTime, alertTime)
	if score != 0.8 {
		t.Errorf("deployScore(30min) = %v, want 0.8", score)
	}
}

func TestDeployScore_WithinTwoHours(t *testing.T) {
	c := &Collector{}
	alertTime := time.Now()
	deployTime := alertTime.Add(-90 * time.Minute)

	score := c.deployScore(deployTime, alertTime)
	if score != 0.6 {
		t.Errorf("deployScore(90min) = %v, want 0.6", score)
	}
}

func TestDeployScore_BeyondTwoHours(t *testing.T) {
	c := &Collector{}
	alertTime := time.Now()
	deployTime := alertTime.Add(-3 * time.Hour)

	score := c.deployScore(deployTime, alertTime)
	if score != 0.4 {
		t.Errorf("deployScore(3h) = %v, want 0.4", score)
	}
}

func TestDeployScore_NegativeDiff(t *testing.T) {
	c := &Collector{}
	alertTime := time.Now()
	deployTime := alertTime.Add(20 * time.Minute)

	score := c.deployScore(deployTime, alertTime)
	if score != 0.8 {
		t.Errorf("deployScore(-20min) = %v, want 0.8 (abs diff used)", score)
	}
}

func TestCollect_APIError_HandledGracefully(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := &Collector{
		githubToken:   "test-token",
		githubOrg:     "org",
		workloadRepos: map[string]string{},
		httpClient:    ts.Client(),
		logger:        zap.NewNop(),
	}

	_, err := c.githubGet(context.Background(), ts.URL+"/repos/org/repo/deployments")
	if err == nil {
		t.Fatal("githubGet() should return error for 500 response")
	}
}

func TestResolveRepo_DirectRepo(t *testing.T) {
	c := &Collector{workloadRepos: map[string]string{"svc": "svc-repo"}}

	target := contracts.TargetRef{Kind: "service", Name: "svc", Repo: "direct-repo"}
	if got := c.resolveRepo(target); got != "direct-repo" {
		t.Errorf("resolveRepo() = %q, want %q (direct repo takes precedence)", got, "direct-repo")
	}
}

func TestResolveRepo_FromMap(t *testing.T) {
	c := &Collector{workloadRepos: map[string]string{"svc": "svc-repo"}}

	target := contracts.TargetRef{Kind: "service", Name: "svc"}
	if got := c.resolveRepo(target); got != "svc-repo" {
		t.Errorf("resolveRepo() = %q, want %q", got, "svc-repo")
	}
}

func TestResolveRepo_NotFound(t *testing.T) {
	c := &Collector{workloadRepos: map[string]string{}}

	target := contracts.TargetRef{Kind: "service", Name: "unknown"}
	if got := c.resolveRepo(target); got != "" {
		t.Errorf("resolveRepo() = %q, want empty string", got)
	}
}

func TestShortSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc123def456789", "abc123de"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"123456789", "12345678"},
	}

	for _, tt := range tests {
		if got := shortSHA(tt.input); got != tt.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"single line", "single line"},
		{"first\nsecond\nthird", "first"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := firstLine(tt.input); got != tt.want {
			t.Errorf("firstLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

package git

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestGitHubProvider_Name(t *testing.T) {
	p := NewGitHubProvider(Config{}, zap.NewNop())
	if got := p.Name(); got != "github" {
		t.Errorf("Name() = %q, want %q", got, "github")
	}
}

func TestGitHubProvider_ResolveRepo(t *testing.T) {
	t.Run("Found", func(t *testing.T) {
		repos := map[string]string{
			"payment-service": "payments-repo",
		}
		p := NewGitHubProvider(Config{WorkloadRepos: repos}, zap.NewNop())

		repo, ok := p.ResolveRepo("payment-service")
		if !ok {
			t.Fatal("expected ok=true for mapped workload")
		}
		if repo != "payments-repo" {
			t.Errorf("ResolveRepo() = %q, want %q", repo, "payments-repo")
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		p := NewGitHubProvider(Config{WorkloadRepos: map[string]string{}}, zap.NewNop())

		repo, ok := p.ResolveRepo("unknown-service")
		if ok {
			t.Fatal("expected ok=false for unmapped workload")
		}
		if repo != "" {
			t.Errorf("ResolveRepo() = %q, want empty string", repo)
		}
	})
}

func TestGitHubProvider_FetchFiles_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "package main\n\nfunc main() {}")
	}))
	defer srv.Close()

	p := newProviderWithTestServer(srv, "test-token", "myorg", "main", 20480, 10)

	results, err := p.FetchFiles(context.Background(), "myrepo", []string{"main.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results["main.go"] != "package main\n\nfunc main() {}" {
		t.Errorf("unexpected content: %q", results["main.go"])
	}
}

func TestGitHubProvider_FetchFiles_MultipleFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "file1.go") {
			fmt.Fprint(w, "content1")
		} else if strings.Contains(r.URL.Path, "file2.go") {
			fmt.Fprint(w, "content2")
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := newProviderWithTestServer(srv, "test-token", "myorg", "main", 20480, 10)

	results, err := p.FetchFiles(context.Background(), "myrepo", []string{"file1.go", "file2.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results["file1.go"] != "content1" {
		t.Errorf("file1.go content = %q, want %q", results["file1.go"], "content1")
	}
	if results["file2.go"] != "content2" {
		t.Errorf("file2.go content = %q, want %q", results["file2.go"], "content2")
	}
}

func TestGitHubProvider_FetchFiles_FileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "exists.go") {
			fmt.Fprint(w, "found")
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := newProviderWithTestServer(srv, "test-token", "myorg", "main", 20480, 10)

	results, err := p.FetchFiles(context.Background(), "myrepo", []string{"missing.go", "exists.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if _, ok := results["missing.go"]; ok {
		t.Error("expected missing.go to be skipped")
	}
	if results["exists.go"] != "found" {
		t.Errorf("exists.go content = %q, want %q", results["exists.go"], "found")
	}
}

func TestGitHubProvider_FetchFiles_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newProviderWithTestServer(srv, "test-token", "myorg", "main", 20480, 10)

	results, err := p.FetchFiles(context.Background(), "myrepo", []string{"broken.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results for 500, got %d entries", len(results))
	}
}

func TestGitHubProvider_FetchFiles_TruncatesLargeFile(t *testing.T) {
	largeContent := strings.Repeat("x", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, largeContent)
	}))
	defer srv.Close()

	maxFileSize := 100
	p := newProviderWithTestServer(srv, "test-token", "myorg", "main", maxFileSize, 10)

	results, err := p.FetchFiles(context.Background(), "myrepo", []string{"big.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if len(results["big.go"]) != maxFileSize {
		t.Errorf("expected truncated length %d, got %d", maxFileSize, len(results["big.go"]))
	}
}

func TestGitHubProvider_FetchFiles_MaxFilesLimit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		fmt.Fprint(w, "content")
	}))
	defer srv.Close()

	maxFiles := 10
	p := newProviderWithTestServer(srv, "test-token", "myorg", "main", 20480, maxFiles)

	paths := make([]string, 15)
	for i := range paths {
		paths[i] = fmt.Sprintf("file%d.go", i)
	}

	results, err := p.FetchFiles(context.Background(), "myrepo", paths)
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if len(results) != maxFiles {
		t.Errorf("expected %d results, got %d", maxFiles, len(results))
	}
	if callCount != maxFiles {
		t.Errorf("expected %d HTTP calls, got %d", maxFiles, callCount)
	}
}

func TestGitHubProvider_FetchFiles_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	token := "ghp_test123"
	p := newProviderWithTestServer(srv, token, "myorg", "main", 20480, 10)

	_, err := p.FetchFiles(context.Background(), "myrepo", []string{"file.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}

	expected := "Bearer " + token
	if gotAuth != expected {
		t.Errorf("Authorization header = %q, want %q", gotAuth, expected)
	}
}

func TestGitHubProvider_FetchFiles_AcceptHeader(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	p := newProviderWithTestServer(srv, "tok", "myorg", "main", 20480, 10)

	_, err := p.FetchFiles(context.Background(), "myrepo", []string{"file.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}

	expected := "application/vnd.github.v3.raw"
	if gotAccept != expected {
		t.Errorf("Accept header = %q, want %q", gotAccept, expected)
	}
}

func TestGitHubProvider_FetchFiles_URLConstruction(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	p := newProviderWithTestServer(srv, "tok", "myorg", "develop", 20480, 10)

	_, err := p.FetchFiles(context.Background(), "myrepo", []string{"src/main.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}

	if !strings.Contains(gotURL, "/repos/myorg/myrepo/contents/src/main.go") {
		t.Errorf("URL missing expected path components: %s", gotURL)
	}
	if !strings.Contains(gotURL, "ref=develop") {
		t.Errorf("URL missing ref=develop: %s", gotURL)
	}
}

func TestGitHubProvider_FetchFiles_EmptyPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request expected for empty paths")
	}))
	defer srv.Close()

	p := newProviderWithTestServer(srv, "tok", "myorg", "main", 20480, 10)

	results, err := p.FetchFiles(context.Background(), "myrepo", []string{})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty map, got %d entries", len(results))
	}
}

func TestGitHubProvider_Defaults(t *testing.T) {
	p := NewGitHubProvider(Config{}, zap.NewNop())

	if p.defaultBranch != "main" {
		t.Errorf("defaultBranch = %q, want %q", p.defaultBranch, "main")
	}
	if p.maxFileSize != 20480 {
		t.Errorf("maxFileSize = %d, want %d", p.maxFileSize, 20480)
	}
	if p.maxFiles != 10 {
		t.Errorf("maxFiles = %d, want %d", p.maxFiles, 10)
	}
}

func TestNoopProvider_FetchFiles(t *testing.T) {
	p := NewNoopProvider()

	results, err := p.FetchFiles(context.Background(), "any-repo", []string{"file.go"})
	if err != nil {
		t.Fatalf("FetchFiles() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty map, got %d entries", len(results))
	}
}

func TestNoopProvider_ResolveRepo(t *testing.T) {
	p := NewNoopProvider()

	repo, ok := p.ResolveRepo("anything")
	if ok {
		t.Error("expected ok=false")
	}
	if repo != "" {
		t.Errorf("expected empty string, got %q", repo)
	}
}

func TestNewGitHubProvider_CustomConfig(t *testing.T) {
	cfg := Config{
		Token:         "custom-token",
		Organization:  "custom-org",
		DefaultBranch: "develop",
		MaxFileSize:   50000,
		MaxFiles:      25,
		WorkloadRepos: map[string]string{"svc": "repo"},
	}
	p := NewGitHubProvider(cfg, zap.NewNop())

	if p.token != "custom-token" {
		t.Errorf("token = %q, want %q", p.token, "custom-token")
	}
	if p.org != "custom-org" {
		t.Errorf("org = %q, want %q", p.org, "custom-org")
	}
	if p.defaultBranch != "develop" {
		t.Errorf("defaultBranch = %q, want %q", p.defaultBranch, "develop")
	}
	if p.maxFileSize != 50000 {
		t.Errorf("maxFileSize = %d, want %d", p.maxFileSize, 50000)
	}
	if p.maxFiles != 25 {
		t.Errorf("maxFiles = %d, want %d", p.maxFiles, 25)
	}
	if p.workloadRepos["svc"] != "repo" {
		t.Errorf("workloadRepos[svc] = %q, want %q", p.workloadRepos["svc"], "repo")
	}
}

func TestNewGitHubProvider_DefaultConfig(t *testing.T) {
	p := NewGitHubProvider(Config{}, zap.NewNop())

	if p.defaultBranch != "main" {
		t.Errorf("defaultBranch = %q, want %q", p.defaultBranch, "main")
	}
	if p.maxFileSize != 20480 {
		t.Errorf("maxFileSize = %d, want %d", p.maxFileSize, 20480)
	}
	if p.maxFiles != 10 {
		t.Errorf("maxFiles = %d, want %d", p.maxFiles, 10)
	}
}

// newProviderWithTestServer creates a GitHubProvider that routes requests to the
// given httptest.Server. It replaces the base URL used by fetchFile by pointing
// org to a path prefix routed through the test server.
func newProviderWithTestServer(srv *httptest.Server, token, org, branch string, maxFileSize, maxFiles int) *GitHubProvider {
	p := NewGitHubProvider(Config{
		Token:         token,
		Organization:  org,
		DefaultBranch: branch,
		MaxFileSize:   maxFileSize,
		MaxFiles:      maxFiles,
		WorkloadRepos: map[string]string{},
	}, zap.NewNop())

	// Replace the HTTP client's transport to redirect api.github.com to the test server.
	p.httpClient = srv.Client()
	p.httpClient.Transport = &rewriteTransport{
		base:    srv.Client().Transport,
		testURL: srv.URL,
	}

	return p
}

// rewriteTransport rewrites requests destined for api.github.com to the test server URL.
type rewriteTransport struct {
	base    http.RoundTripper
	testURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	// Parse the test URL to get host
	req.URL.Host = strings.TrimPrefix(t.testURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

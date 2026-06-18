package git

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type Provider interface {
	FetchFiles(ctx context.Context, repo string, paths []string) (map[string]string, error)
	ResolveRepo(workload string) (string, bool)
}

type Config struct {
	Provider      string
	Token         string
	Organization  string
	WorkloadRepos map[string]string
	DefaultBranch string
	MaxFileSize   int
	MaxFiles      int
}

type GitHubProvider struct {
	token         string
	org           string
	workloadRepos map[string]string
	defaultBranch string
	maxFileSize   int
	maxFiles      int
	httpClient    *http.Client
	logger        *zap.Logger
}

func NewGitHubProvider(cfg Config, logger *zap.Logger) *GitHubProvider {
	branch := cfg.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	maxFileSize := cfg.MaxFileSize
	if maxFileSize <= 0 {
		maxFileSize = 20480
	}
	maxFiles := cfg.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 10
	}

	return &GitHubProvider{
		token:         cfg.Token,
		org:           cfg.Organization,
		workloadRepos: cfg.WorkloadRepos,
		defaultBranch: branch,
		maxFileSize:   maxFileSize,
		maxFiles:      maxFiles,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (p *GitHubProvider) Name() string {
	return "github"
}

func (p *GitHubProvider) ResolveRepo(workload string) (string, bool) {
	repo, ok := p.workloadRepos[workload]
	return repo, ok
}

func (p *GitHubProvider) FetchFiles(ctx context.Context, repo string, paths []string) (map[string]string, error) {
	if len(paths) > p.maxFiles {
		p.logger.Warn("trimming file list to max allowed",
			zap.Int("requested", len(paths)),
			zap.Int("max", p.maxFiles),
		)
		paths = paths[:p.maxFiles]
	}

	results := make(map[string]string, len(paths))

	for _, path := range paths {
		content, err := p.fetchFile(ctx, repo, path)
		if err != nil {
			p.logger.Warn("skipping file",
				zap.String("repo", repo),
				zap.String("path", path),
				zap.Error(err),
			)
			continue
		}
		results[path] = content
	}

	return results, nil
}

func (p *GitHubProvider) fetchFile(ctx context.Context, repo, path string) (string, error) {
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		url.PathEscape(p.org), url.PathEscape(repo), path, url.QueryEscape(p.defaultBranch))

	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Accept", "application/vnd.github.v3.raw")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetching %s: %w", path, err)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return "", lastErr
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d for %s", resp.StatusCode, path)
			if attempt < maxRetries {
				retryAfter := resp.Header.Get("Retry-After")
				delay := time.Duration(attempt+1) * time.Second
				if retryAfter != "" {
					if secs, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
						delay = time.Duration(secs) * time.Second
					}
				}
				time.Sleep(delay)
				continue
			}
			return "", lastErr
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return "", fmt.Errorf("file not found: %s", path)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("unexpected status %d for %s", resp.StatusCode, path)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("reading response for %s: %w", path, err)
		}

		content := string(body)
		if len(content) > p.maxFileSize {
			p.logger.Warn("truncating oversized file",
				zap.String("path", path),
				zap.Int("original_size", len(content)),
				zap.Int("max_size", p.maxFileSize),
			)
			content = content[:p.maxFileSize]
		}

		return content, nil
	}
	return "", lastErr
}

// NoopProvider is a no-op fallback used when Git integration is disabled.
type NoopProvider struct{}

func NewNoopProvider() *NoopProvider {
	return &NoopProvider{}
}

func (p *NoopProvider) FetchFiles(_ context.Context, _ string, _ []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (p *NoopProvider) ResolveRepo(_ string) (string, bool) {
	return "", false
}

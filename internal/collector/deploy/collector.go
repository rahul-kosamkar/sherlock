package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

const sourceName = "deploy"

type Config struct {
	GitHubToken   string
	GitLabToken   string
	GitHubOrg     string
	WorkloadRepos map[string]string
}

type Collector struct {
	githubToken   string
	gitlabToken   string
	githubOrg     string
	workloadRepos map[string]string
	httpClient    *http.Client
	logger        *zap.Logger
}

func New(cfg Config, logger *zap.Logger) *Collector {
	return &Collector{
		githubToken:   cfg.GitHubToken,
		gitlabToken:   cfg.GitLabToken,
		githubOrg:     cfg.GitHubOrg,
		workloadRepos: cfg.WorkloadRepos,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (c *Collector) Name() string { return sourceName }

func (c *Collector) Collect(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	evidence := make([]contracts.Evidence, 0, len(req.Targets))

	for _, target := range req.Targets {
		repo := c.resolveRepo(target)
		if repo == "" {
			continue
		}

		env := target.Environment
		if env == "" {
			env = "production"
		}

		deployEvidence, err := c.collectGitHubDeployments(ctx, req, target, repo, env)
		if err != nil {
			c.logger.Warn("github deployments query failed",
				zap.String("repo", repo),
				zap.Error(err),
			)
			if c.gitlabToken != "" {
				glEvidence, glErr := c.collectGitLabDeployments(ctx, req, target, repo, env)
				if glErr != nil {
					c.logger.Warn("gitlab deployments query also failed",
						zap.String("repo", repo),
						zap.Error(glErr),
					)
				} else {
					evidence = append(evidence, glEvidence...)
				}
			}
			continue
		}

		evidence = append(evidence, deployEvidence...)
	}

	return evidence, nil
}

func (c *Collector) resolveRepo(target contracts.TargetRef) string {
	if target.Repo != "" {
		return target.Repo
	}
	if repo, ok := c.workloadRepos[target.Name]; ok {
		return repo
	}
	return ""
}

func (c *Collector) collectGitHubDeployments(ctx context.Context, req contracts.CollectRequest, target contracts.TargetRef, repo, env string) ([]contracts.Evidence, error) {
	if c.githubToken == "" {
		return nil, fmt.Errorf("github token not configured")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/deployments?per_page=5&environment=%s",
		c.githubOrg, repo, env)

	body, err := c.githubGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch deployments: %w", err)
	}

	var deployments []ghDeployment
	if err := json.Unmarshal(body, &deployments); err != nil {
		return nil, fmt.Errorf("decode deployments: %w", err)
	}

	extendedFrom := req.TimeFrom.Add(-2 * time.Hour)
	evidence := make([]contracts.Evidence, 0, len(deployments))
	var prevSHA string

	for i, dep := range deployments {
		if dep.CreatedAt.Before(extendedFrom) {
			if prevSHA == "" {
				prevSHA = dep.SHA
			}
			continue
		}
		if dep.CreatedAt.After(req.TimeTo) {
			continue
		}

		score := c.deployScore(dep.CreatedAt, req.Alert.StartsAt)

		status := c.fetchDeploymentStatus(ctx, dep.StatusesURL)

		ev := contracts.Evidence{
			ID:              uuid.NewString(),
			InvestigationID: req.InvestigationID,
			Kind:            contracts.EvidenceDeploy,
			Source:          sourceName,
			Target:          target,
			CollectedAt:     time.Now().UTC(),
			ObservedAtFrom:  dep.CreatedAt,
			ObservedAtTo:    dep.CreatedAt,
			Summary:         fmt.Sprintf("Deployment of %s to %s by %s at %s", shortSHA(dep.SHA), dep.Environment, dep.Creator.Login, dep.CreatedAt.Format(time.RFC3339)),
			Query:           url,
			Score:           score,
			Attributes: map[string]string{
				"sha":         dep.SHA,
				"ref":         dep.Ref,
				"environment": dep.Environment,
				"creator":     dep.Creator.Login,
				"status":      status,
				"description": dep.Description,
			},
			RedactionState: contracts.RedactionNone,
		}
		evidence = append(evidence, ev)

		if i+1 < len(deployments) {
			prevSHA = deployments[i+1].SHA
		}

		if prevSHA != "" && prevSHA != dep.SHA {
			compareEvidence, compErr := c.collectGitHubCompare(ctx, req, target, repo, prevSHA, dep.SHA)
			if compErr != nil {
				c.logger.Warn("github compare failed",
					zap.String("repo", repo),
					zap.Error(compErr),
				)
			} else {
				evidence = append(evidence, compareEvidence...)
			}
		}
	}

	return evidence, nil
}

func (c *Collector) collectGitHubCompare(ctx context.Context, req contracts.CollectRequest, target contracts.TargetRef, repo, base, head string) ([]contracts.Evidence, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/compare/%s...%s",
		c.githubOrg, repo, base, head)

	body, err := c.githubGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch compare: %w", err)
	}

	var comparison ghComparison
	if err := json.Unmarshal(body, &comparison); err != nil {
		return nil, fmt.Errorf("decode compare: %w", err)
	}

	evidence := make([]contracts.Evidence, 0, 1+len(comparison.Commits))

	summaryEv := contracts.Evidence{
		ID:              uuid.NewString(),
		InvestigationID: req.InvestigationID,
		Kind:            contracts.EvidenceGitChange,
		Source:          sourceName,
		Target:          target,
		CollectedAt:     time.Now().UTC(),
		ObservedAtFrom:  req.TimeFrom,
		ObservedAtTo:    req.TimeTo,
		Summary: fmt.Sprintf("Deployment diff %s...%s: %d files changed, +%d -%d across %d commits",
			shortSHA(base), shortSHA(head),
			len(comparison.Files), comparison.AheadBy, comparison.BehindBy, comparison.TotalCommits),
		Query: url,
		Score: 0.7,
		Attributes: map[string]string{
			"files_changed": strconv.Itoa(len(comparison.Files)),
			"additions":     strconv.Itoa(comparison.Additions()),
			"deletions":     strconv.Itoa(comparison.Deletions()),
			"commit_count":  strconv.Itoa(comparison.TotalCommits),
			"compare_url":   comparison.HTMLURL,
			"base_sha":      base,
			"head_sha":      head,
		},
		RedactionState: contracts.RedactionNone,
	}
	evidence = append(evidence, summaryEv)

	for _, commit := range comparison.Commits {
		if len(commit.Commit.Message) < 10 {
			continue
		}
		commitEv := contracts.Evidence{
			ID:              uuid.NewString(),
			InvestigationID: req.InvestigationID,
			Kind:            contracts.EvidenceGitChange,
			Source:          sourceName,
			Target:          target,
			CollectedAt:     time.Now().UTC(),
			ObservedAtFrom:  req.TimeFrom,
			ObservedAtTo:    req.TimeTo,
			Summary:         fmt.Sprintf("Commit %s: %s", shortSHA(commit.SHA), firstLine(commit.Commit.Message)),
			Score:           0.5,
			Attributes: map[string]string{
				"sha":     commit.SHA,
				"author":  commit.Commit.Author.Name,
				"message": commit.Commit.Message,
			},
			RedactionState: contracts.RedactionNone,
		}
		evidence = append(evidence, commitEv)
	}

	return evidence, nil
}

func (c *Collector) collectGitLabDeployments(ctx context.Context, req contracts.CollectRequest, target contracts.TargetRef, repo, env string) ([]contracts.Evidence, error) {
	if c.gitlabToken == "" {
		return nil, fmt.Errorf("gitlab token not configured")
	}

	url := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/deployments?per_page=5&environment=%s&order_by=created_at&sort=desc",
		repo, env)

	body, err := c.gitlabGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch gitlab deployments: %w", err)
	}

	var deployments []glDeployment
	if err := json.Unmarshal(body, &deployments); err != nil {
		return nil, fmt.Errorf("decode gitlab deployments: %w", err)
	}

	extendedFrom := req.TimeFrom.Add(-2 * time.Hour)
	evidence := make([]contracts.Evidence, 0, len(deployments))

	for _, dep := range deployments {
		if dep.CreatedAt.Before(extendedFrom) || dep.CreatedAt.After(req.TimeTo) {
			continue
		}

		score := c.deployScore(dep.CreatedAt, req.Alert.StartsAt)

		ev := contracts.Evidence{
			ID:              uuid.NewString(),
			InvestigationID: req.InvestigationID,
			Kind:            contracts.EvidenceDeploy,
			Source:          sourceName,
			Target:          target,
			CollectedAt:     time.Now().UTC(),
			ObservedAtFrom:  dep.CreatedAt,
			ObservedAtTo:    dep.CreatedAt,
			Summary:         fmt.Sprintf("GitLab deployment of %s to %s by %s at %s", shortSHA(dep.SHA), dep.Environment, dep.User.Username, dep.CreatedAt.Format(time.RFC3339)),
			Query:           url,
			Score:           score,
			Attributes: map[string]string{
				"sha":         dep.SHA,
				"ref":         dep.Ref,
				"environment": dep.Environment,
				"creator":     dep.User.Username,
				"status":      dep.Status,
			},
			RedactionState: contracts.RedactionNone,
		}
		evidence = append(evidence, ev)
	}

	return evidence, nil
}

func (c *Collector) deployScore(deployTime, alertTime time.Time) float64 {
	diff := alertTime.Sub(deployTime)
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff <= time.Hour:
		return 0.8
	case diff <= 2*time.Hour:
		return 0.6
	default:
		return 0.4
	}
}

func (c *Collector) fetchDeploymentStatus(ctx context.Context, statusesURL string) string {
	if statusesURL == "" {
		return "unknown"
	}
	body, err := c.githubGet(ctx, statusesURL)
	if err != nil {
		return "unknown"
	}

	var statuses []ghDeploymentStatus
	if err := json.Unmarshal(body, &statuses); err != nil || len(statuses) == 0 {
		return "unknown"
	}
	return statuses[0].State
}

func (c *Collector) doWithRetry(_ context.Context, req *http.Request) (*http.Response, error) {
	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return nil, lastErr
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if attempt < maxRetries {
				retryAfter := resp.Header.Get("Retry-After")
				delay := time.Duration(attempt+1) * time.Second
				if retryAfter != "" {
					if secs, err := strconv.Atoi(retryAfter); err == nil {
						delay = time.Duration(secs) * time.Second
					}
				}
				time.Sleep(delay)
				continue
			}
			return nil, lastErr
		}
		return resp, nil
	}
	return nil, lastErr
}

func (c *Collector) githubGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.githubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d for %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

func (c *Collector) gitlabGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.gitlabToken)

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab API returned %d for %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

// GitHub API response types

type ghDeployment struct {
	ID          int64     `json:"id"`
	SHA         string    `json:"sha"`
	Ref         string    `json:"ref"`
	Environment string    `json:"environment"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	Creator     struct {
		Login string `json:"login"`
	} `json:"creator"`
	StatusesURL string `json:"statuses_url"`
}

type ghDeploymentStatus struct {
	State string `json:"state"`
}

type ghComparison struct {
	HTMLURL      string     `json:"html_url"`
	AheadBy      int        `json:"ahead_by"`
	BehindBy     int        `json:"behind_by"`
	TotalCommits int        `json:"total_commits"`
	Commits      []ghCommit `json:"commits"`
	Files        []ghFile   `json:"files"`
}

func (c *ghComparison) Additions() int {
	total := 0
	for _, f := range c.Files {
		total += f.Additions
	}
	return total
}

func (c *ghComparison) Deletions() int {
	total := 0
	for _, f := range c.Files {
		total += f.Deletions
	}
	return total
}

type ghCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

type ghFile struct {
	Filename  string `json:"filename"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
}

// GitLab API response types

type glDeployment struct {
	ID          int       `json:"id"`
	SHA         string    `json:"sha"`
	Ref         string    `json:"ref"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	User        struct {
		Username string `json:"username"`
	} `json:"user"`
}

// Utility functions

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func firstLine(s string) string {
	for i, ch := range s {
		if ch == '\n' {
			return s[:i]
		}
	}
	return s
}

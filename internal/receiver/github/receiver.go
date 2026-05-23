package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

// --- GitHub webhook payload types ---

type repository struct {
	FullName string `json:"full_name"`
	Name     string `json:"name"`
	HTMLURL  string `json:"html_url"`
}

type sender struct {
	Login string `json:"login"`
}

type deployment struct {
	ID          int64  `json:"id"`
	SHA         string `json:"sha"`
	Ref         string `json:"ref"`
	Environment string `json:"environment"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Creator     sender `json:"creator"`
}

type deploymentEvent struct {
	Action     string     `json:"action"`
	Deployment deployment `json:"deployment"`
	Repository repository `json:"repository"`
	Sender     sender     `json:"sender"`
}

type deploymentState struct {
	ID            int64      `json:"id"`
	State         string     `json:"state"`
	Description   string     `json:"description"`
	TargetURL     string     `json:"target_url"`
	EnvironmentURL string    `json:"environment_url"`
	Deployment    deployment `json:"deployment"`
}

type deploymentStatusEvent struct {
	DeploymentStatus deploymentState `json:"deployment_status"`
	Deployment       deployment      `json:"deployment"`
	Repository       repository      `json:"repository"`
	Sender           sender          `json:"sender"`
}

type commit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	URL     string `json:"url"`
	Author  struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"author"`
}

type pushEvent struct {
	Ref        string     `json:"ref"`
	Before     string     `json:"before"`
	After      string     `json:"after"`
	Compare    string     `json:"compare"`
	HeadCommit *commit    `json:"head_commit"`
	Commits    []commit   `json:"commits"`
	Repository repository `json:"repository"`
	Sender     sender     `json:"sender"`
}

// --- Receiver ---

type Receiver struct {
	hmacSecret []byte
}

func New(webhookSecret string) *Receiver {
	return &Receiver{hmacSecret: []byte(webhookSecret)}
}

func (r *Receiver) Source() string { return "github" }

func (r *Receiver) Verify(_ context.Context, headers http.Header, body []byte) error {
	if len(r.hmacSecret) == 0 {
		return nil
	}

	sig := headers.Get("X-Hub-Signature-256")
	if sig == "" {
		return fmt.Errorf("missing X-Hub-Signature-256 header")
	}

	if !strings.HasPrefix(sig, "sha256=") {
		return fmt.Errorf("malformed X-Hub-Signature-256 header")
	}
	sig = strings.TrimPrefix(sig, "sha256=")

	mac := hmac.New(sha256.New, r.hmacSecret)
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return fmt.Errorf("HMAC signature mismatch")
	}
	return nil
}

func (r *Receiver) Decode(_ context.Context, headers http.Header, body []byte) ([]contracts.NormalizedAlert, error) {
	event := headers.Get("X-GitHub-Event")
	switch event {
	case "deployment":
		return r.decodeDeployment(body)
	case "deployment_status":
		return r.decodeDeploymentStatus(body)
	case "push":
		return r.decodePush(body)
	default:
		return nil, nil
	}
}

func (r *Receiver) decodeDeployment(body []byte) ([]contracts.NormalizedAlert, error) {
	var ev deploymentEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("unmarshalling github deployment event: %w", err)
	}

	d := ev.Deployment
	repo := ev.Repository.FullName
	env := d.Environment
	sha := d.SHA

	summary := d.Description
	if summary == "" {
		summary = fmt.Sprintf("Deployment of %s to %s", shortSHA(sha), env)
	}

	na := contracts.NormalizedAlert{
		ID:       uuid.NewString(),
		Source:   "github",
		Status:   contracts.AlertStatusFiring,
		Severity: contracts.SeverityInfo,
		Title:    fmt.Sprintf("Deployment to %s — %s", env, repo),
		Summary:  summary,
		Fingerprint: fingerprint(repo, env, sha),
		StartsAt: time.Now().UTC(),
		Labels: map[string]string{
			"repo":        repo,
			"environment": env,
			"ref":         d.Ref,
			"sha":         sha,
			"sender":      ev.Sender.Login,
		},
		EntityHints: []contracts.TargetRef{
			{
				Kind:        "repo",
				Name:        ev.Repository.Name,
				Repo:        repo,
				Environment: env,
			},
		},
		Links: buildDeploymentLinks(d, ev.Repository),
	}

	return []contracts.NormalizedAlert{na}, nil
}

func (r *Receiver) decodeDeploymentStatus(body []byte) ([]contracts.NormalizedAlert, error) {
	var ev deploymentStatusEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("unmarshalling github deployment_status event: %w", err)
	}

	state := strings.ToLower(ev.DeploymentStatus.State)
	if state != "failure" && state != "error" {
		return nil, nil
	}

	d := ev.Deployment
	repo := ev.Repository.FullName
	env := d.Environment
	sha := d.SHA

	na := contracts.NormalizedAlert{
		ID:       uuid.NewString(),
		Source:   "github",
		Status:   contracts.AlertStatusFiring,
		Severity: contracts.SeverityWarning,
		Title:    fmt.Sprintf("Deployment %s — %s/%s", state, repo, env),
		Summary:  ev.DeploymentStatus.Description,
		Fingerprint: fingerprint(repo, env, sha),
		StartsAt: time.Now().UTC(),
		Labels: map[string]string{
			"repo":        repo,
			"environment": env,
			"ref":         d.Ref,
			"sha":         sha,
			"status":      state,
			"sender":      ev.Sender.Login,
		},
		EntityHints: []contracts.TargetRef{
			{
				Kind:        "repo",
				Name:        ev.Repository.Name,
				Repo:        repo,
				Environment: env,
			},
		},
		Links: buildDeploymentStatusLinks(ev),
	}

	return []contracts.NormalizedAlert{na}, nil
}

func (r *Receiver) decodePush(body []byte) ([]contracts.NormalizedAlert, error) {
	var ev pushEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("unmarshalling github push event: %w", err)
	}

	repo := ev.Repository.FullName
	ref := ev.Ref

	summary := fmt.Sprintf("%d commits pushed to %s", len(ev.Commits), ref)
	if ev.HeadCommit != nil && ev.HeadCommit.Message != "" {
		summary = ev.HeadCommit.Message
	}

	headSHA := ev.After
	labels := map[string]string{
		"repo":   repo,
		"ref":    ref,
		"sender": ev.Sender.Login,
	}
	if headSHA != "" {
		labels["head_commit"] = headSHA
	}

	var links []contracts.Link
	if ev.Compare != "" {
		links = append(links, contracts.Link{Rel: "compare", Href: ev.Compare})
	}

	na := contracts.NormalizedAlert{
		ID:       uuid.NewString(),
		Source:   "github",
		Status:   contracts.AlertStatusFiring,
		Severity: contracts.SeverityInfo,
		Title:    fmt.Sprintf("Push to %s — %s", ref, repo),
		Summary:  summary,
		Fingerprint: fingerprint(repo, ref, ev.After),
		StartsAt: time.Now().UTC(),
		Labels:  labels,
		EntityHints: []contracts.TargetRef{
			{
				Kind: "repo",
				Name: ev.Repository.Name,
				Repo: repo,
			},
		},
		Links: links,
	}

	return []contracts.NormalizedAlert{na}, nil
}

// --- helpers ---

func fingerprint(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:])
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func buildDeploymentLinks(d deployment, repo repository) []contracts.Link {
	var links []contracts.Link
	if d.URL != "" {
		links = append(links, contracts.Link{Rel: "deployment", Href: d.URL})
	}
	if repo.HTMLURL != "" {
		links = append(links, contracts.Link{Rel: "repository", Href: repo.HTMLURL})
	}
	return links
}

func buildDeploymentStatusLinks(ev deploymentStatusEvent) []contracts.Link {
	var links []contracts.Link
	if ev.DeploymentStatus.TargetURL != "" {
		links = append(links, contracts.Link{Rel: "target", Href: ev.DeploymentStatus.TargetURL})
	}
	if ev.Deployment.URL != "" {
		links = append(links, contracts.Link{Rel: "deployment", Href: ev.Deployment.URL})
	}
	if ev.Repository.HTMLURL != "" {
		links = append(links, contracts.Link{Rel: "repository", Href: ev.Repository.HTMLURL})
	}
	return links
}

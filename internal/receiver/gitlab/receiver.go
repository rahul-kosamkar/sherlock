package gitlab

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

// --- GitLab webhook payload types ---

type project struct {
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
}

type commit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	URL     string `json:"url"`
	Author  struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
}

type deploymentHookEvent struct {
	ObjectKind   string  `json:"object_kind"`
	Status       string  `json:"status"`
	DeployableID int64   `json:"deployable_id"`
	Environment  string  `json:"environment"`
	ShortSHA     string  `json:"short_sha"`
	CommitURL    string  `json:"commit_url"`
	CommitTitle  string  `json:"commit_title"`
	User         string  `json:"user"`
	UserUsername string  `json:"user_username"`
	Project      project `json:"project"`
}

type pushHookEvent struct {
	ObjectKind   string   `json:"object_kind"`
	Ref          string   `json:"ref"`
	Before       string   `json:"before"`
	After        string   `json:"after"`
	UserName     string   `json:"user_name"`
	UserUsername string   `json:"user_username"`
	Commits      []commit `json:"commits"`
	Project      project  `json:"project"`
}

// --- Receiver ---

type Receiver struct {
	secretToken string
}

func New(secretToken string) *Receiver {
	return &Receiver{secretToken: secretToken}
}

func (r *Receiver) Source() string { return "gitlab" }

func (r *Receiver) Verify(_ context.Context, headers http.Header, _ []byte) error {
	if r.secretToken == "" {
		return nil
	}

	token := headers.Get("X-Gitlab-Token")
	if token == "" {
		return fmt.Errorf("missing X-Gitlab-Token header")
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(r.secretToken)) != 1 {
		return fmt.Errorf("secret token mismatch")
	}
	return nil
}

func (r *Receiver) Decode(_ context.Context, headers http.Header, body []byte) ([]contracts.NormalizedAlert, error) {
	event := headers.Get("X-Gitlab-Event")
	switch event {
	case "Deployment Hook":
		return r.decodeDeployment(body)
	case "Push Hook":
		return r.decodePush(body)
	default:
		return nil, nil
	}
}

func (r *Receiver) decodeDeployment(body []byte) ([]contracts.NormalizedAlert, error) {
	var ev deploymentHookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("unmarshalling gitlab deployment hook: %w", err)
	}

	status := strings.ToLower(ev.Status)
	if status != "running" && status != "failed" && status != "created" {
		return nil, nil
	}

	proj := ev.Project.PathWithNamespace
	env := ev.Environment
	sha := ev.ShortSHA

	severity := contracts.SeverityInfo
	if status == "failed" {
		severity = contracts.SeverityWarning
	}

	na := contracts.NormalizedAlert{
		ID:          uuid.NewString(),
		Source:      "gitlab",
		Status:      contracts.AlertStatusFiring,
		Severity:    severity,
		Title:       fmt.Sprintf("Deployment %s — %s/%s", status, ev.Project.Name, env),
		Summary:     fmt.Sprintf("Deployment of %s to %s", sha, env),
		Fingerprint: fingerprint(proj, env, sha),
		StartsAt:    time.Now().UTC(),
		Labels: map[string]string{
			"project":     proj,
			"environment": env,
			"status":      status,
			"sha":         sha,
			"user":        ev.UserUsername,
		},
		EntityHints: []contracts.TargetRef{
			{
				Kind:        "repo",
				Name:        ev.Project.Name,
				Repo:        proj,
				Environment: env,
			},
		},
		Links: buildDeploymentLinks(ev),
	}

	return []contracts.NormalizedAlert{na}, nil
}

func (r *Receiver) decodePush(body []byte) ([]contracts.NormalizedAlert, error) {
	var ev pushHookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("unmarshalling gitlab push hook: %w", err)
	}

	proj := ev.Project.PathWithNamespace
	ref := ev.Ref

	summary := fmt.Sprintf("%d commits pushed to %s", len(ev.Commits), ref)
	if len(ev.Commits) > 0 {
		last := ev.Commits[len(ev.Commits)-1]
		if last.Message != "" {
			summary = last.Message
		}
	}

	labels := map[string]string{
		"project":   proj,
		"ref":       ref,
		"user_name": ev.UserName,
		"sha":       ev.After,
	}

	var links []contracts.Link
	if ev.Project.WebURL != "" {
		links = append(links, contracts.Link{Rel: "project", Href: ev.Project.WebURL})
	}

	na := contracts.NormalizedAlert{
		ID:          uuid.NewString(),
		Source:      "gitlab",
		Status:      contracts.AlertStatusFiring,
		Severity:    contracts.SeverityInfo,
		Title:       fmt.Sprintf("Push to %s — %s", ref, ev.Project.Name),
		Summary:     summary,
		Fingerprint: fingerprint(proj, ref, ev.After),
		StartsAt:    time.Now().UTC(),
		Labels:      labels,
		EntityHints: []contracts.TargetRef{
			{
				Kind: "repo",
				Name: ev.Project.Name,
				Repo: proj,
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

func buildDeploymentLinks(ev deploymentHookEvent) []contracts.Link {
	var links []contracts.Link
	if ev.CommitURL != "" {
		links = append(links, contracts.Link{Rel: "commit", Href: ev.CommitURL})
	}
	if ev.Project.WebURL != "" {
		links = append(links, contracts.Link{Rel: "project", Href: ev.Project.WebURL})
	}
	return links
}

package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for defaults()
// ---------------------------------------------------------------------------

func TestDefaults_LLM(t *testing.T) {
	cfg := defaults()

	if cfg.LLM.Enabled != false {
		t.Errorf("LLM.Enabled = %v, want false", cfg.LLM.Enabled)
	}
	if cfg.LLM.Provider != "openai" {
		t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "openai")
	}
	if cfg.LLM.Model != "gpt-4o" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "gpt-4o")
	}
	if cfg.LLM.Temperature != 0.2 {
		t.Errorf("LLM.Temperature = %f, want %f", cfg.LLM.Temperature, 0.2)
	}
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("LLM.MaxTokens = %d, want %d", cfg.LLM.MaxTokens, 4096)
	}
	if cfg.LLM.MaxPasses != 3 {
		t.Errorf("LLM.MaxPasses = %d, want %d", cfg.LLM.MaxPasses, 3)
	}
	if cfg.LLM.Timeout != 120*time.Second {
		t.Errorf("LLM.Timeout = %v, want %v", cfg.LLM.Timeout, 120*time.Second)
	}
}

func TestDefaults_Git(t *testing.T) {
	cfg := defaults()

	if cfg.Git.Enabled != false {
		t.Errorf("Git.Enabled = %v, want false", cfg.Git.Enabled)
	}
	if cfg.Git.Provider != "github" {
		t.Errorf("Git.Provider = %q, want %q", cfg.Git.Provider, "github")
	}
	if cfg.Git.DefaultBranch != "main" {
		t.Errorf("Git.DefaultBranch = %q, want %q", cfg.Git.DefaultBranch, "main")
	}
	if cfg.Git.WorkloadRepos == nil {
		t.Fatal("Git.WorkloadRepos should not be nil")
	}
	if len(cfg.Git.WorkloadRepos) != 0 {
		t.Errorf("Git.WorkloadRepos should be empty, got %d entries", len(cfg.Git.WorkloadRepos))
	}
}

func TestDefaults_Server(t *testing.T) {
	cfg := defaults()

	if cfg.Server.Address != ":8080" {
		t.Errorf("Server.Address = %q, want %q", cfg.Server.Address, ":8080")
	}
}

func TestDefaults_Postgres(t *testing.T) {
	cfg := defaults()

	if !strings.Contains(cfg.Postgres.DSN, "sherlock") {
		t.Errorf("Postgres.DSN = %q, want it to contain %q", cfg.Postgres.DSN, "sherlock")
	}
	if cfg.Postgres.MaxConns != 10 {
		t.Errorf("Postgres.MaxConns = %d, want %d", cfg.Postgres.MaxConns, 10)
	}
}

func TestDefaults_Investigation(t *testing.T) {
	cfg := defaults()

	if cfg.Investigation.Timeout != 5*time.Minute {
		t.Errorf("Investigation.Timeout = %v, want %v", cfg.Investigation.Timeout, 5*time.Minute)
	}
	if cfg.Investigation.MaxConcurrent != 10 {
		t.Errorf("Investigation.MaxConcurrent = %d, want %d", cfg.Investigation.MaxConcurrent, 10)
	}
}

// ---------------------------------------------------------------------------
// Tests for Load with env var overrides
// ---------------------------------------------------------------------------

func TestLoad_LLMEnvOverrides(t *testing.T) {
	t.Setenv("SHERLOCK_LLM_ENABLED", "true")
	t.Setenv("SHERLOCK_LLM_PROVIDER", "anthropic")
	t.Setenv("SHERLOCK_LLM_MODEL", "claude-3")
	t.Setenv("SHERLOCK_LLM_API_KEY", "sk-test")
	t.Setenv("SHERLOCK_LLM_ENDPOINT", "http://custom")
	t.Setenv("SHERLOCK_LLM_GCP_PROJECT", "proj")
	t.Setenv("SHERLOCK_LLM_GCP_REGION", "us-west1")
	t.Setenv("SHERLOCK_LLM_TEMPERATURE", "0.5")
	t.Setenv("SHERLOCK_LLM_MAX_TOKENS", "8192")
	t.Setenv("SHERLOCK_LLM_MAX_PASSES", "5")
	t.Setenv("SHERLOCK_LLM_TIMEOUT", "60s")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.LLM.Enabled != true {
		t.Errorf("LLM.Enabled = %v, want true", cfg.LLM.Enabled)
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "anthropic")
	}
	if cfg.LLM.Model != "claude-3" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "claude-3")
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("LLM.APIKey = %q, want %q", cfg.LLM.APIKey, "sk-test")
	}
	if cfg.LLM.Endpoint != "http://custom" {
		t.Errorf("LLM.Endpoint = %q, want %q", cfg.LLM.Endpoint, "http://custom")
	}
	if cfg.LLM.GCPProject != "proj" {
		t.Errorf("LLM.GCPProject = %q, want %q", cfg.LLM.GCPProject, "proj")
	}
	if cfg.LLM.GCPRegion != "us-west1" {
		t.Errorf("LLM.GCPRegion = %q, want %q", cfg.LLM.GCPRegion, "us-west1")
	}
	if cfg.LLM.Temperature != 0.5 {
		t.Errorf("LLM.Temperature = %f, want %f", cfg.LLM.Temperature, 0.5)
	}
	if cfg.LLM.MaxTokens != 8192 {
		t.Errorf("LLM.MaxTokens = %d, want %d", cfg.LLM.MaxTokens, 8192)
	}
	if cfg.LLM.MaxPasses != 5 {
		t.Errorf("LLM.MaxPasses = %d, want %d", cfg.LLM.MaxPasses, 5)
	}
	if cfg.LLM.Timeout != 60*time.Second {
		t.Errorf("LLM.Timeout = %v, want %v", cfg.LLM.Timeout, 60*time.Second)
	}
}

func TestLoad_GitEnvOverrides(t *testing.T) {
	t.Setenv("SHERLOCK_GIT_ENABLED", "true")
	t.Setenv("SHERLOCK_GIT_PROVIDER", "github")
	t.Setenv("SHERLOCK_GIT_TOKEN", "ghp-test")
	t.Setenv("SHERLOCK_GIT_ORGANIZATION", "myorg")
	t.Setenv("SHERLOCK_GIT_DEFAULT_BRANCH", "develop")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Git.Enabled != true {
		t.Errorf("Git.Enabled = %v, want true", cfg.Git.Enabled)
	}
	if cfg.Git.Provider != "github" {
		t.Errorf("Git.Provider = %q, want %q", cfg.Git.Provider, "github")
	}
	if cfg.Git.Token != "ghp-test" {
		t.Errorf("Git.Token = %q, want %q", cfg.Git.Token, "ghp-test")
	}
	if cfg.Git.Organization != "myorg" {
		t.Errorf("Git.Organization = %q, want %q", cfg.Git.Organization, "myorg")
	}
	if cfg.Git.DefaultBranch != "develop" {
		t.Errorf("Git.DefaultBranch = %q, want %q", cfg.Git.DefaultBranch, "develop")
	}
}

func TestLoad_LLMEnabled_CaseInsensitive(t *testing.T) {
	t.Run("TRUE", func(t *testing.T) {
		t.Setenv("SHERLOCK_LLM_ENABLED", "TRUE")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.Enabled != true {
			t.Errorf("LLM.Enabled = %v, want true", cfg.LLM.Enabled)
		}
	})

	t.Run("True", func(t *testing.T) {
		t.Setenv("SHERLOCK_LLM_ENABLED", "True")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.Enabled != true {
			t.Errorf("LLM.Enabled = %v, want true", cfg.LLM.Enabled)
		}
	})
}

func TestLoad_LLMEnabled_False(t *testing.T) {
	t.Setenv("SHERLOCK_LLM_ENABLED", "false")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Enabled != false {
		t.Errorf("LLM.Enabled = %v, want false", cfg.LLM.Enabled)
	}
}

func TestLoad_InvalidTemperature(t *testing.T) {
	t.Setenv("SHERLOCK_LLM_TEMPERATURE", "notanumber")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Temperature != 0.2 {
		t.Errorf("LLM.Temperature = %f, want default %f", cfg.LLM.Temperature, 0.2)
	}
}

func TestLoad_InvalidMaxTokens(t *testing.T) {
	t.Setenv("SHERLOCK_LLM_MAX_TOKENS", "abc")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("LLM.MaxTokens = %d, want default %d", cfg.LLM.MaxTokens, 4096)
	}
}

func TestLoad_InvalidTimeout(t *testing.T) {
	t.Setenv("SHERLOCK_LLM_TIMEOUT", "notaduration")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Timeout != 120*time.Second {
		t.Errorf("LLM.Timeout = %v, want default %v", cfg.LLM.Timeout, 120*time.Second)
	}
}

func TestLoad_ExistingEnvOverrides(t *testing.T) {
	t.Setenv("SHERLOCK_SERVER_ADDRESS", ":9090")
	t.Setenv("SHERLOCK_POSTGRES_DSN", "postgres://custom:custom@db:5432/custom")
	t.Setenv("SHERLOCK_NATS_URL", "nats://custom:4222")
	t.Setenv("SHERLOCK_SLACK_MODE", "http")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Address != ":9090" {
		t.Errorf("Server.Address = %q, want %q", cfg.Server.Address, ":9090")
	}
	if cfg.Postgres.DSN != "postgres://custom:custom@db:5432/custom" {
		t.Errorf("Postgres.DSN = %q, want custom DSN", cfg.Postgres.DSN)
	}
	if cfg.NATS.URL != "nats://custom:4222" {
		t.Errorf("NATS.URL = %q, want %q", cfg.NATS.URL, "nats://custom:4222")
	}
	if cfg.Slack.Mode != "http" {
		t.Errorf("Slack.Mode = %q, want %q", cfg.Slack.Mode, "http")
	}
}

// ---------------------------------------------------------------------------
// Tests for Load with YAML files
// ---------------------------------------------------------------------------

func TestLoad_YAMLFile(t *testing.T) {
	content := `
server:
  address: ":9999"
postgres:
  dsn: "postgres://yaml:yaml@localhost/yaml"
  max_conns: 20
nats:
  url: "nats://yaml:4222"
  stream_name: "YAML_STREAM"
slack:
  mode: "both"
llm:
  enabled: true
  provider: "anthropic"
  model: "claude-3-opus"
  temperature: 0.7
  max_tokens: 2048
  max_passes: 5
  timeout: 30s
git:
  enabled: true
  provider: "github"
  token: "yaml-token"
  organization: "yaml-org"
  default_branch: "release"
  workload_repos:
    my-svc: "my-repo"
`
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Address != ":9999" {
		t.Errorf("Server.Address = %q, want %q", cfg.Server.Address, ":9999")
	}
	if cfg.Postgres.DSN != "postgres://yaml:yaml@localhost/yaml" {
		t.Errorf("Postgres.DSN = %q", cfg.Postgres.DSN)
	}
	if cfg.Postgres.MaxConns != 20 {
		t.Errorf("Postgres.MaxConns = %d, want %d", cfg.Postgres.MaxConns, 20)
	}
	if cfg.LLM.Enabled != true {
		t.Errorf("LLM.Enabled = %v, want true", cfg.LLM.Enabled)
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "anthropic")
	}
	if cfg.LLM.Model != "claude-3-opus" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "claude-3-opus")
	}
	if cfg.LLM.Temperature != 0.7 {
		t.Errorf("LLM.Temperature = %f, want %f", cfg.LLM.Temperature, 0.7)
	}
	if cfg.LLM.MaxTokens != 2048 {
		t.Errorf("LLM.MaxTokens = %d, want %d", cfg.LLM.MaxTokens, 2048)
	}
	if cfg.LLM.MaxPasses != 5 {
		t.Errorf("LLM.MaxPasses = %d, want %d", cfg.LLM.MaxPasses, 5)
	}
	if cfg.LLM.Timeout != 30*time.Second {
		t.Errorf("LLM.Timeout = %v, want %v", cfg.LLM.Timeout, 30*time.Second)
	}
	if cfg.Git.Enabled != true {
		t.Errorf("Git.Enabled = %v, want true", cfg.Git.Enabled)
	}
	if cfg.Git.Token != "yaml-token" {
		t.Errorf("Git.Token = %q, want %q", cfg.Git.Token, "yaml-token")
	}
	if cfg.Git.Organization != "yaml-org" {
		t.Errorf("Git.Organization = %q, want %q", cfg.Git.Organization, "yaml-org")
	}
	if cfg.Git.DefaultBranch != "release" {
		t.Errorf("Git.DefaultBranch = %q, want %q", cfg.Git.DefaultBranch, "release")
	}
	if cfg.Git.WorkloadRepos["my-svc"] != "my-repo" {
		t.Errorf("Git.WorkloadRepos[my-svc] = %q, want %q", cfg.Git.WorkloadRepos["my-svc"], "my-repo")
	}
}

func TestLoad_YAMLFile_NotFound(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (should use defaults)", err)
	}
	if cfg.Server.Address != ":8080" {
		t.Errorf("Server.Address = %q, want default %q", cfg.Server.Address, ":8080")
	}
}

func TestLoad_YAMLFile_InvalidYAML(t *testing.T) {
	content := `
server:
  address: [invalid yaml
  this is broken: {{{
`
	f, err := os.CreateTemp(t.TempDir(), "bad-config-*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	f.Close()

	_, err = Load(f.Name())
	if err == nil {
		t.Fatal("Load() expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing config file") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "parsing config file")
	}
}

// ---------------------------------------------------------------------------
// Tests for validate()
// ---------------------------------------------------------------------------

func TestValidate_Valid(t *testing.T) {
	cfg := defaults()
	if err := validate(&cfg); err != nil {
		t.Errorf("validate() error = %v, want nil", err)
	}
}

func TestValidate_EmptyAddress(t *testing.T) {
	cfg := defaults()
	cfg.Server.Address = ""
	if err := validate(&cfg); err == nil {
		t.Fatal("validate() expected error for empty address, got nil")
	}
}

func TestValidate_EmptyDSN(t *testing.T) {
	cfg := defaults()
	cfg.Postgres.DSN = ""
	if err := validate(&cfg); err == nil {
		t.Fatal("validate() expected error for empty DSN, got nil")
	}
}

func TestValidate_EmptyNATSURL(t *testing.T) {
	cfg := defaults()
	cfg.NATS.URL = ""
	if err := validate(&cfg); err == nil {
		t.Fatal("validate() expected error for empty NATS URL, got nil")
	}
}

func TestValidate_InvalidSlackMode(t *testing.T) {
	cfg := defaults()
	cfg.Slack.Mode = "invalid"
	if err := validate(&cfg); err == nil {
		t.Fatal("validate() expected error for invalid slack mode, got nil")
	}
}

func TestValidate_ValidSlackModes(t *testing.T) {
	for _, mode := range []string{"socket", "http", "both"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg := defaults()
			cfg.Slack.Mode = mode
			if err := validate(&cfg); err != nil {
				t.Errorf("validate() error = %v for mode %q", err, mode)
			}
		})
	}
}

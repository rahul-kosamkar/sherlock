package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Postgres      PostgresConfig      `yaml:"postgres"`
	NATS          NATSConfig          `yaml:"nats"`
	ObjectStore   ObjectStoreConfig   `yaml:"objectstore"`
	Slack         SlackConfig         `yaml:"slack"`
	Collectors    CollectorsConfig    `yaml:"collectors"`
	Receivers     ReceiversConfig     `yaml:"receivers"`
	Investigation InvestigationConfig `yaml:"investigation"`
	Dedup         DedupConfig         `yaml:"dedup"`
	LLM           LLMConfig           `yaml:"llm"`
	Git           GitConfig           `yaml:"git"`
	Remediation   RemediationConfig   `yaml:"remediation"`
	Tracing       TracingConfig       `yaml:"tracing"`
	AutoMigrate   bool                `yaml:"auto_migrate"`
}

type DedupConfig struct {
	Enabled bool          `yaml:"enabled"`
	Window  time.Duration `yaml:"window"`
}

type ServerConfig struct {
	Address        string  `yaml:"address"`
	RateLimitRPS   float64 `yaml:"rate_limit_rps"`
	RateLimitBurst int     `yaml:"rate_limit_burst"`
	APIKey         string  `yaml:"api_key"`
}

type PostgresConfig struct {
	DSN      string `yaml:"dsn"`
	MaxConns int    `yaml:"max_conns"`
}

type NATSConfig struct {
	URL        string `yaml:"url"`
	StreamName string `yaml:"stream_name"`
}

type ObjectStoreConfig struct {
	Endpoint     string `yaml:"endpoint"`
	Bucket       string `yaml:"bucket"`
	Region       string `yaml:"region"`
	AccessKey    string `yaml:"access_key"`
	SecretKey    string `yaml:"secret_key"`
	UsePathStyle bool   `yaml:"use_path_style"`
}

type SlackConfig struct {
	BotToken      string `yaml:"bot_token"`
	AppToken      string `yaml:"app_token"`
	SigningSecret string `yaml:"signing_secret"`
	Mode          string `yaml:"mode"` // socket, http, both
}

type CollectorsConfig struct {
	Prometheus PrometheusConfig `yaml:"prometheus"`
	Loki       LokiConfig       `yaml:"loki"`
	Kubernetes KubernetesConfig `yaml:"kubernetes"`
}

type PrometheusConfig struct {
	URL string `yaml:"url"`
}

type LokiConfig struct {
	URL string `yaml:"url"`
}

type KubernetesConfig struct {
	InCluster  bool   `yaml:"in_cluster"`
	Kubeconfig string `yaml:"kubeconfig"`
}

type ReceiversConfig struct {
	Grafana      GrafanaReceiverConfig      `yaml:"grafana"`
	Alertmanager AlertmanagerReceiverConfig `yaml:"alertmanager"`
	GitHub       GitHubReceiverConfig       `yaml:"github"`
	GitLab       GitLabReceiverConfig       `yaml:"gitlab"`
}

type GrafanaReceiverConfig struct {
	Enabled    bool   `yaml:"enabled"`
	HMACSecret string `yaml:"hmac_secret"`
}

type AlertmanagerReceiverConfig struct {
	Enabled bool `yaml:"enabled"`
}

type GitHubReceiverConfig struct {
	Enabled       bool   `yaml:"enabled"`
	WebhookSecret string `yaml:"webhook_secret"`
}

type GitLabReceiverConfig struct {
	Enabled     bool   `yaml:"enabled"`
	SecretToken string `yaml:"secret_token"`
}

type InvestigationConfig struct {
	Timeout       time.Duration `yaml:"timeout"`
	MaxConcurrent int           `yaml:"max_concurrent"`
}

type LLMConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Provider    string        `yaml:"provider"`
	Model       string        `yaml:"model"`
	APIKey      string        `yaml:"api_key"`
	Endpoint    string        `yaml:"endpoint"`
	GCPProject  string        `yaml:"gcp_project"`
	GCPRegion   string        `yaml:"gcp_region"`
	Temperature float32       `yaml:"temperature"`
	MaxTokens   int           `yaml:"max_tokens"`
	MaxPasses   int           `yaml:"max_passes"`
	Timeout     time.Duration `yaml:"timeout"`
}

type GitConfig struct {
	Enabled       bool              `yaml:"enabled"`
	Provider      string            `yaml:"provider"`
	Token         string            `yaml:"token"`
	Organization  string            `yaml:"organization"`
	DefaultBranch string            `yaml:"default_branch"`
	WorkloadRepos map[string]string `yaml:"workload_repos"`
}

type RemediationConfig struct {
	Enabled      bool   `yaml:"enabled"`
	PoliciesPath string `yaml:"policies_path"`
}

type TracingConfig struct {
	Enabled    bool    `yaml:"enabled"`
	Endpoint   string  `yaml:"endpoint"`
	SampleRate float64 `yaml:"sample_rate"`
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Address:        ":8080",
			RateLimitRPS:   50,
			RateLimitBurst: 100,
		},
		Postgres: PostgresConfig{
			DSN:      "postgres://sherlock:sherlock@localhost:5432/sherlock?sslmode=disable",
			MaxConns: 10,
		},
		NATS: NATSConfig{
			URL:        "nats://localhost:4222",
			StreamName: "INVESTIGATIONS",
		},
		ObjectStore: ObjectStoreConfig{
			Endpoint:     "http://localhost:9000",
			Bucket:       "sherlock",
			Region:       "us-east-1",
			UsePathStyle: true,
		},
		Slack: SlackConfig{
			Mode: "socket",
		},
		Collectors: CollectorsConfig{
			Prometheus: PrometheusConfig{URL: "http://localhost:9090"},
			Loki:       LokiConfig{URL: "http://localhost:3100"},
			Kubernetes: KubernetesConfig{InCluster: false},
		},
		Receivers: ReceiversConfig{
			Grafana:      GrafanaReceiverConfig{Enabled: true},
			Alertmanager: AlertmanagerReceiverConfig{Enabled: true},
			GitHub:       GitHubReceiverConfig{Enabled: false},
			GitLab:       GitLabReceiverConfig{Enabled: false},
		},
		Investigation: InvestigationConfig{
			Timeout:       5 * time.Minute,
			MaxConcurrent: 10,
		},
		Dedup: DedupConfig{
			Enabled: true,
			Window:  1 * time.Hour,
		},
		LLM: LLMConfig{
			Enabled:     false,
			Provider:    "openai",
			Model:       "gpt-4o",
			Temperature: 0.2,
			MaxTokens:   4096,
			MaxPasses:   3,
			Timeout:     120 * time.Second,
		},
		Git: GitConfig{
			Enabled:       false,
			Provider:      "github",
			DefaultBranch: "main",
			WorkloadRepos: make(map[string]string),
		},
		Remediation: RemediationConfig{
			Enabled: true,
		},
		Tracing: TracingConfig{
			Enabled:    false,
			Endpoint:   "localhost:4318",
			SampleRate: 1.0,
		},
	}
}

// Load reads configuration from a YAML file (if it exists) and
// applies environment variable overrides on top.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		if err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("parsing config file: %w", err)
			}
		}
	}

	applyEnvOverrides(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SHERLOCK_SERVER_ADDRESS"); v != "" {
		cfg.Server.Address = v
	}
	if v := os.Getenv("SHERLOCK_RATE_LIMIT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Server.RateLimitRPS = f
		}
	}
	if v := os.Getenv("SHERLOCK_RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.RateLimitBurst = n
		}
	}
	if v := os.Getenv("SHERLOCK_API_KEY"); v != "" {
		cfg.Server.APIKey = v
	}
	if v := os.Getenv("SHERLOCK_POSTGRES_DSN"); v != "" {
		cfg.Postgres.DSN = v
	}
	if v := os.Getenv("SHERLOCK_POSTGRES_MAX_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Postgres.MaxConns = n
		}
	}
	if v := os.Getenv("SHERLOCK_NATS_URL"); v != "" {
		cfg.NATS.URL = v
	}
	if v := os.Getenv("SHERLOCK_NATS_STREAM_NAME"); v != "" {
		cfg.NATS.StreamName = v
	}
	if v := os.Getenv("SHERLOCK_S3_ENDPOINT"); v != "" {
		cfg.ObjectStore.Endpoint = v
	}
	if v := os.Getenv("SHERLOCK_S3_BUCKET"); v != "" {
		cfg.ObjectStore.Bucket = v
	}
	if v := os.Getenv("SHERLOCK_S3_REGION"); v != "" {
		cfg.ObjectStore.Region = v
	}
	if v := os.Getenv("SHERLOCK_S3_ACCESS_KEY"); v != "" {
		cfg.ObjectStore.AccessKey = v
	}
	if v := os.Getenv("SHERLOCK_S3_SECRET_KEY"); v != "" {
		cfg.ObjectStore.SecretKey = v
	}
	if v := os.Getenv("SHERLOCK_SLACK_BOT_TOKEN"); v != "" {
		cfg.Slack.BotToken = v
	}
	if v := os.Getenv("SHERLOCK_SLACK_APP_TOKEN"); v != "" {
		cfg.Slack.AppToken = v
	}
	if v := os.Getenv("SHERLOCK_SLACK_SIGNING_SECRET"); v != "" {
		cfg.Slack.SigningSecret = v
	}
	if v := os.Getenv("SHERLOCK_SLACK_MODE"); v != "" {
		cfg.Slack.Mode = v
	}
	if v := os.Getenv("SHERLOCK_PROMETHEUS_URL"); v != "" {
		cfg.Collectors.Prometheus.URL = v
	}
	if v := os.Getenv("SHERLOCK_LOKI_URL"); v != "" {
		cfg.Collectors.Loki.URL = v
	}
	if v := os.Getenv("SHERLOCK_K8S_IN_CLUSTER"); v != "" {
		cfg.Collectors.Kubernetes.InCluster = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("SHERLOCK_K8S_KUBECONFIG"); v != "" {
		cfg.Collectors.Kubernetes.Kubeconfig = v
	}
	if v := os.Getenv("SHERLOCK_GRAFANA_HMAC_SECRET"); v != "" {
		cfg.Receivers.Grafana.HMACSecret = v
	}
	if v := os.Getenv("SHERLOCK_GITHUB_WEBHOOK_SECRET"); v != "" {
		cfg.Receivers.GitHub.WebhookSecret = v
	}
	if v := os.Getenv("SHERLOCK_GITLAB_SECRET_TOKEN"); v != "" {
		cfg.Receivers.GitLab.SecretToken = v
	}
	if v := os.Getenv("SHERLOCK_INVESTIGATION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Investigation.Timeout = d
		}
	}
	if v := os.Getenv("SHERLOCK_INVESTIGATION_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Investigation.MaxConcurrent = n
		}
	}

	if v := os.Getenv("SHERLOCK_DEDUP_ENABLED"); v != "" {
		cfg.Dedup.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("SHERLOCK_DEDUP_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Dedup.Window = d
		}
	}

	if v := os.Getenv("SHERLOCK_LLM_ENABLED"); v != "" {
		cfg.LLM.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("SHERLOCK_LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("SHERLOCK_LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("SHERLOCK_LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("SHERLOCK_LLM_ENDPOINT"); v != "" {
		cfg.LLM.Endpoint = v
	}
	if v := os.Getenv("SHERLOCK_LLM_GCP_PROJECT"); v != "" {
		cfg.LLM.GCPProject = v
	}
	if v := os.Getenv("SHERLOCK_LLM_GCP_REGION"); v != "" {
		cfg.LLM.GCPRegion = v
	}
	if v := os.Getenv("SHERLOCK_LLM_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			cfg.LLM.Temperature = float32(f)
		}
	}
	if v := os.Getenv("SHERLOCK_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LLM.MaxTokens = n
		}
	}
	if v := os.Getenv("SHERLOCK_LLM_MAX_PASSES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LLM.MaxPasses = n
		}
	}
	if v := os.Getenv("SHERLOCK_LLM_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.LLM.Timeout = d
		}
	}

	if v := os.Getenv("SHERLOCK_GIT_ENABLED"); v != "" {
		cfg.Git.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("SHERLOCK_GIT_PROVIDER"); v != "" {
		cfg.Git.Provider = v
	}
	if v := os.Getenv("SHERLOCK_GIT_TOKEN"); v != "" {
		cfg.Git.Token = v
	}
	if v := os.Getenv("SHERLOCK_GIT_ORGANIZATION"); v != "" {
		cfg.Git.Organization = v
	}
	if v := os.Getenv("SHERLOCK_GIT_DEFAULT_BRANCH"); v != "" {
		cfg.Git.DefaultBranch = v
	}

	if v := os.Getenv("SHERLOCK_REMEDIATION_ENABLED"); v != "" {
		cfg.Remediation.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("SHERLOCK_REMEDIATION_POLICIES_PATH"); v != "" {
		cfg.Remediation.PoliciesPath = v
	}

	if v := os.Getenv("SHERLOCK_TRACING_ENABLED"); v != "" {
		cfg.Tracing.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("SHERLOCK_TRACING_ENDPOINT"); v != "" {
		cfg.Tracing.Endpoint = v
	}
	if v := os.Getenv("SHERLOCK_TRACING_SAMPLE_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Tracing.SampleRate = f
		}
	}

	if v := os.Getenv("SHERLOCK_AUTO_MIGRATE"); v != "" {
		cfg.AutoMigrate = strings.EqualFold(v, "true")
	}
}

func validate(cfg *Config) error {
	if cfg.Server.Address == "" {
		return fmt.Errorf("server.address is required")
	}
	if cfg.Postgres.DSN == "" {
		return fmt.Errorf("postgres.dsn is required")
	}
	if cfg.NATS.URL == "" {
		return fmt.Errorf("nats.url is required")
	}
	mode := cfg.Slack.Mode
	if mode != "socket" && mode != "http" && mode != "both" {
		return fmt.Errorf("slack.mode must be one of: socket, http, both")
	}
	return nil
}

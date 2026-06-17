# Sherlock

**Turn noisy alerts into evidence-backed, auditable incident investigations in Slack.**

Sherlock is an open-source incident investigator written in Go. It sits between your alerting systems and your engineering team, automatically pulling evidence from logs, metrics, Kubernetes, deployments, and source code to produce ranked, evidence-backed hypotheses -- delivered directly in Slack threads.

When LLM integration is enabled, Sherlock performs **multi-pass AI-powered deep investigations** -- iteratively gathering evidence, requesting targeted follow-up data, and tracing root causes down to specific code paths and functions.

## Why Sherlock?

When an alert fires, engineers waste time context-switching between dashboards, log viewers, cluster state, and deployment history. Sherlock automates that first investigation pass:

- Receives alerts via webhooks from Grafana, Alertmanager, GitHub, and GitLab
- Pulls evidence from Prometheus metrics, Loki logs, Kubernetes cluster state, and deployment history
- Correlates deployment changes with alert onset ("what changed?")
- Runs root cause analysis -- rule-based by default, or **LLM-powered multi-pass** for deeper investigations
- Deduplicates alerts to avoid repeated investigations
- Applies remediation policies to suggest context-aware fixes
- Publishes a concise, inspectable investigation result in Slack with linked evidence
- Every conclusion is linked to specific evidence records -- never just prose

### With LLM Enabled (Optional)

- **Multi-pass investigation**: The LLM forms a hypothesis, requests specific follow-up data (trace logs, pod events, source code), and refines its analysis across 2-3 passes
- **Code-level root cause**: Traces failures to specific functions, error handling patterns, and code paths
- **Actionable fix recommendations**: Provides concrete steps to resolve the issue
- **Confidence scoring**: Each investigation reports HIGH/MEDIUM/LOW confidence with supporting evidence
- **Provider-agnostic**: Works with OpenAI, Google Vertex AI (Gemini), Anthropic, or local Ollama models

## Architecture

```
Alert Sources ──► Receiver Gateway ──► Dedup ──► NATS JetStream ──► Investigation Orchestrator
(Grafana,              │                                                     │
 Alertmanager,         ▼                                          ┌──────────┼──────────┐
 GitHub,          S3 (raw payloads)                                ▼          ▼          ▼
 GitLab)                                                         K8s   Prometheus  Loki/Deploy
                                                                  │          │          │
                                                                  ▼          ▼          ▼
                                                              Evidence Store (Postgres)
                                                                        │
                                                              ┌─────────┼─────────┐
                                                              ▼         ▼         ▼
                                                        Correlation   RCA    Timeline
                                                              │         │         │
                                                              └─────────┼─────────┘
                                                                        ▼
                                                              Remediation Policies
                                                                        │
                                                                        ▼
                                                                Slack Publisher
```

When LLM is enabled, the RCA step becomes a multi-pass loop:

```
Evidence ──► Prompt Builder ──► LLM Provider ──► Response Parser
                  ▲                                     │
                  │                              FOLLOW_UP requests
                  │                                     │
                  └──── Follow-Up Executor ◄────────────┘
                              │
                    ┌─────────┼─────────┐
                    ▼         ▼         ▼
                  Loki      K8s     Git Provider
               (traces)  (events)  (source code)
```

## Features

### Alert Sources

| Source | Webhook Path | Verification | Status |
|--------|-------------|--------------|--------|
| Grafana Alerting | `/webhooks/grafana` | HMAC-SHA256 | Supported |
| Prometheus Alertmanager | `/webhooks/alertmanager` | -- | Supported |
| GitHub (deploys, pushes) | `/webhooks/github` | HMAC-SHA256 (`X-Hub-Signature-256`) | Supported |
| GitLab (deploys, pushes) | `/webhooks/gitlab` | Secret token (`X-Gitlab-Token`) | Supported |

### Evidence Collectors

| Collector | Evidence Types | Description |
|-----------|---------------|-------------|
| Kubernetes | Pod status, events, previous logs | CrashLoop, OOM, scheduling failures |
| Prometheus | CPU, memory, restarts, error rates | Metric anomaly detection |
| Loki | Error logs, crash signals, stack traces | LogQL-based log analysis |
| Deploy | Deployments, commit diffs | GitHub/GitLab deployment correlation |

### RCA Rules

| Rule | Detects | Cause Category |
|------|---------|----------------|
| CrashLoop | Container crash loop patterns | code / deploy |
| OOMKilled | Out-of-memory kills | capacity |
| HighCPU | CPU spikes (traffic vs deploy) | capacity / deploy |
| ErrorSpike | Elevated error rates | code / deploy |
| SchedulingFailure | K8s scheduling / node issues | infra |
| DeployProximity | Recent deploy correlated with alert | deploy |

### Slack Interactions

| Surface | Description |
|---------|-------------|
| `/investigate <service>` | Ad hoc investigation from slash command |
| Message shortcut | "Investigate this alert" from any message |
| `@Sherlock investigate <service>` | App mention to start investigation |
| `@Sherlock status <id>` | Check investigation status |
| Re-run Investigation | Button to re-run with fresh data |
| View Evidence | Button to expand all evidence in-thread |
| Open Runbook | Button to link to runbook URL from alert annotations |
| Suppress Similar | Button to suppress duplicate alerts for 1 hour |
| Compare Deployments | Button to show recent deploy diffs |
| Create Issue | Button to generate a copyable issue template |
| Force Re-investigate | Override dedup and force a new investigation |

### Incident Deduplication

Sherlock automatically detects duplicate alerts using fingerprint matching. When a duplicate arrives within the configured window (default: 1 hour):

- The alert is linked to the existing investigation
- A Slack notification is posted with a "Force Re-investigate" option
- No redundant investigation is triggered

Configure via:
```bash
SHERLOCK_DEDUP_ENABLED=true     # default: true
SHERLOCK_DEDUP_WINDOW=1h        # default: 1h
```

### Remediation Policies

Sherlock includes a policy engine that automatically suggests remediation actions based on the root cause category and confidence level. Built-in policies cover:

- **Deploy regression** -- Rollback and diff review when deploy correlation is high
- **OOM / capacity** -- Memory limit increases and leak profiling
- **Code bugs** -- Error pattern review and fix PR creation
- **Infrastructure** -- Node health checks and resource quota review

Custom policies can be loaded from a YAML file:
```yaml
policies:
  - name: rollback-on-deploy-regression
    match:
      cause_category: deploy
      confidence_min: 0.7
    actions:
      - title: "Rollback to previous version"
        description: "Use your deployment tool to rollback to the last known good version."
        safe_by_default: false
      - title: "Compare with previous deploy"
        description: "Review the diff between current and previous deployment."
        safe_by_default: true
```

## Quick Start

### Prerequisites

- Docker and Docker Compose
- A Slack workspace where you can create apps

### 1. Clone and start services

```bash
git clone https://github.com/rahulkosamkar/sherlock.git
cd sherlock
make dev
```

This starts Postgres, MinIO (S3-compatible), NATS, and Sherlock.

### 2. Create a Slack app

Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App > From a manifest**. Paste the contents of `slack-app-manifest.yml` from this repo.

### 3. Configure tokens

Copy the Bot Token and App Token from your Slack app settings:

```bash
cp .env.example .env
# Edit .env with your Slack tokens
```

### 4. Connect alert sources

Point your alert sources to Sherlock:

| Source | Webhook URL |
|--------|------------|
| Grafana | `http://<sherlock-host>:8080/webhooks/grafana` |
| Alertmanager | `http://<sherlock-host>:8080/webhooks/alertmanager` |
| GitHub | `http://<sherlock-host>:8080/webhooks/github` |
| GitLab | `http://<sherlock-host>:8080/webhooks/gitlab` |

### 5. Investigate

Use `/investigate <service-name>` in Slack, mention `@Sherlock investigate <service>`, or right-click any alert message and select **Investigate this alert**.

## Database Migrations

Sherlock uses `golang-migrate` for schema management:

```bash
# Apply all migrations
sherlock migrate up

# Rollback
sherlock migrate down

# Check current version
sherlock migrate version
```

Auto-migrate on startup (recommended for development):
```bash
SHERLOCK_AUTO_MIGRATE=true
```

## Enabling LLM-Powered Investigations (Optional)

By default, Sherlock uses rule-based analysis (CrashLoop, OOM, ErrorSpike, etc.). To enable deeper AI-powered investigations, configure an LLM provider.

### Environment Variables

```bash
SHERLOCK_LLM_ENABLED=true
SHERLOCK_LLM_PROVIDER=openai          # openai | vertex | anthropic | ollama
SHERLOCK_LLM_MODEL=gpt-4o
SHERLOCK_LLM_API_KEY=sk-...
SHERLOCK_LLM_TEMPERATURE=0.2          # default: 0.2
SHERLOCK_LLM_MAX_TOKENS=4096          # default: 4096
SHERLOCK_LLM_MAX_PASSES=3             # default: 3
SHERLOCK_LLM_TIMEOUT=120s             # default: 120s
```

### Provider-Specific Configuration

**OpenAI**
```bash
SHERLOCK_LLM_PROVIDER=openai
SHERLOCK_LLM_MODEL=gpt-4o
SHERLOCK_LLM_API_KEY=sk-...
# Optional: SHERLOCK_LLM_ENDPOINT for Azure OpenAI or compatible APIs
```

**Google Vertex AI (Gemini)**
```bash
SHERLOCK_LLM_PROVIDER=vertex
SHERLOCK_LLM_MODEL=gemini-2.5-pro
SHERLOCK_LLM_GCP_PROJECT=my-project
SHERLOCK_LLM_GCP_REGION=us-central1
# Uses Application Default Credentials (ADC) -- no API key needed
```

**Anthropic**
```bash
SHERLOCK_LLM_PROVIDER=anthropic
SHERLOCK_LLM_MODEL=claude-sonnet-4-20250514
SHERLOCK_LLM_API_KEY=sk-ant-...
```

**Ollama (Local/Self-Hosted)**
```bash
SHERLOCK_LLM_PROVIDER=ollama
SHERLOCK_LLM_MODEL=llama3
SHERLOCK_LLM_ENDPOINT=http://localhost:11434
```

## Enabling Git Source Code Analysis (Optional)

When configured, the LLM can request specific source files from your repositories to trace bugs to exact code paths. The deploy collector also uses this configuration to correlate deployments with alerts.

```bash
SHERLOCK_GIT_ENABLED=true
SHERLOCK_GIT_PROVIDER=github
SHERLOCK_GIT_TOKEN=ghp_...
SHERLOCK_GIT_ORGANIZATION=your-org
SHERLOCK_GIT_DEFAULT_BRANCH=main
```

Map workloads to repositories in `config.yaml`:

```yaml
git:
  enabled: true
  provider: github
  token: ${SHERLOCK_GIT_TOKEN}
  organization: your-org
  default_branch: main
  workload_repos:
    payment-service: payment-service
    user-service: user-service
    order-api: order-api-v2
```

## GitHub / GitLab Webhook Setup

### GitHub

1. Go to your repo **Settings > Webhooks > Add webhook**
2. Set Payload URL to `http://<sherlock-host>:8080/webhooks/github`
3. Set Content type to `application/json`
4. Set a Secret and configure `SHERLOCK_GITHUB_WEBHOOK_SECRET` to match
5. Select events: **Deployments**, **Deployment statuses**, **Pushes**

### GitLab

1. Go to your project **Settings > Webhooks > Add new webhook**
2. Set URL to `http://<sherlock-host>:8080/webhooks/gitlab`
3. Set a Secret token and configure `SHERLOCK_GITLAB_SECRET_TOKEN` to match
4. Select triggers: **Deployment events**, **Push events**

## How Multi-Pass Investigation Works

When LLM is enabled, Sherlock runs investigations in multiple passes:

**Pass 1 -- Initial Hypothesis**
The LLM receives all collected evidence (pod status, events, logs, metrics, deployment diffs) and forms an initial hypothesis. It must also request specific follow-up data.

**Pass 2 -- Deep Analysis**
Sherlock executes the follow-up requests (trace logs, pod events, source code) and feeds the results back to the LLM. The LLM refines its hypothesis with the new evidence.

**Pass 3 (Optional) -- Final Refinement**
If confidence is still medium or low, the LLM can request one more round of data.

Each pass is reported in the Slack thread in real time.

### Available Follow-Up Tools

| Tool | Description | Example |
|------|-------------|---------|
| `TRACE_LOGS` | Fetch complete log trail for trace IDs | `TRACE_LOGS: abc123, def456` |
| `TIME_WINDOW_LOGS` | Fetch all logs in a time window | `TIME_WINDOW_LOGS: 2024-01-15T10:00:00Z/2024-01-15T10:05:00Z` |
| `POD_EVENTS` | Fetch fresh Kubernetes events | `POD_EVENTS: all` |
| `GITHUB_FILES` | Fetch source code files | `GITHUB_FILES: src/handler.go, pkg/service.go` |
| `LOG_QUERY` | Run a custom Loki LogQL query | `LOG_QUERY: {app="payment"} \|= "timeout"` |

## Observability

### Prometheus Metrics

Sherlock exposes a `/metrics` endpoint with Prometheus-compatible metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `sherlock_webhooks_received_total` | Counter | Webhooks by source and status (accepted/rejected/decode_error) |
| `sherlock_webhook_duration_seconds` | Histogram | Webhook processing latency |
| `sherlock_investigations_started_total` | Counter | Total investigations started |
| `sherlock_investigations_completed_total` | Counter | Investigations completed by status (done/failed) |
| `sherlock_investigation_duration_seconds` | Histogram | End-to-end investigation duration |
| `sherlock_active_investigations` | Gauge | Currently running investigations |
| `sherlock_llm_calls_total` | Counter | LLM API calls by provider and status |
| `sherlock_llm_call_duration_seconds` | Histogram | LLM call latency |
| `sherlock_dedup_hits_total` | Counter | Duplicate alerts detected |
| `sherlock_suppress_hits_total` | Counter | Suppressed alerts |
| `sherlock_evidence_collected_total` | Counter | Evidence items by collector |

Scrape config for Prometheus:
```yaml
scrape_configs:
  - job_name: sherlock
    static_configs:
      - targets: ["sherlock:8080"]
```

### Distributed Tracing (OpenTelemetry)

Sherlock supports OpenTelemetry distributed tracing, allowing you to trace an alert from webhook ingestion through evidence collection, RCA, and Slack delivery.

```bash
SHERLOCK_TRACING_ENABLED=true
SHERLOCK_TRACING_ENDPOINT=localhost:4318   # OTLP HTTP endpoint (Jaeger, Tempo, etc.)
SHERLOCK_TRACING_SAMPLE_RATE=1.0           # 0.0 to 1.0
```

Spans are created for:
- `webhook.receive` -- alert ingestion with verify, decode, dedup, and publish events
- `investigation.run` -- full investigation lifecycle
- `investigation.collect` -- evidence collection phase
- `investigation.correlate` -- correlation engine
- `investigation.rca` -- root cause analysis
- `investigation.publish` -- Slack result delivery

## Security

### API Authentication

The investigation API (`/api/v1/*`) supports API key authentication. When configured, all requests (except `/api/v1/health`) require a valid API key:

```bash
SHERLOCK_API_KEY=your-secret-api-key
```

Clients authenticate via:
```bash
# Bearer token
curl -H "Authorization: Bearer your-secret-api-key" http://sherlock:8080/api/v1/investigations/INV-123

# Query parameter
curl http://sherlock:8080/api/v1/investigations/INV-123?api_key=your-secret-api-key
```

When `SHERLOCK_API_KEY` is empty (default), the API is unauthenticated -- suitable for cluster-internal access behind a network policy.

### Rate Limiting

Webhook endpoints are rate-limited per source IP to prevent flood attacks:

```bash
SHERLOCK_RATE_LIMIT_RPS=50     # requests per second per IP (default: 50)
SHERLOCK_RATE_LIMIT_BURST=100  # burst capacity per IP (default: 100)
```

Requests exceeding the limit receive HTTP 429 (Too Many Requests).

### Webhook Verification

All webhook sources support signature verification:
- **Grafana**: HMAC-SHA256 via `SHERLOCK_GRAFANA_HMAC_SECRET`
- **GitHub**: HMAC-SHA256 via `SHERLOCK_GITHUB_WEBHOOK_SECRET`
- **GitLab**: Secret token via `SHERLOCK_GITLAB_SECRET_TOKEN`

## Building from Source

```bash
make build            # Build the binary
make test             # Run tests
make lint             # Run golangci-lint
make coverage         # Run tests with coverage report
make docker-build     # Build Docker image
make release-dry-run  # Test goreleaser locally
```

## Deployment

### Docker Compose (development)

```bash
make dev
```

### Kubernetes (production)

```bash
helm install sherlock charts/sherlock \
  --set slack.botToken=xoxb-... \
  --set slack.appToken=xapp-... \
  --set slack.signingSecret=... \
  --set llm.enabled=true \
  --set llm.provider=openai \
  --set llm.apiKey=sk-... \
  --set git.enabled=true \
  --set git.token=ghp-...
```

See `charts/sherlock/values.yaml` for all configurable values.

#### Autoscaling

Enable HorizontalPodAutoscaler for production workloads:

```yaml
# values.yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80
```

#### Pod Disruption Budget

Ensure availability during cluster maintenance:

```yaml
# values.yaml
podDisruptionBudget:
  enabled: true
  minAvailable: 1
```

### Releases

Tagged releases automatically build and publish:
- Multi-arch binaries (linux/darwin, amd64/arm64) via GoReleaser
- Docker images to `ghcr.io/rahulkosamkar/sherlock`

To create a release:
```bash
git tag v1.0.0
git push origin v1.0.0
```

## Configuration Reference

Sherlock is configured via `config.yaml` with environment variable overrides. All environment variables use the `SHERLOCK_` prefix.

### Core

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_SERVER_ADDRESS` | HTTP server bind address | `:8080` |
| `SHERLOCK_POSTGRES_DSN` | PostgreSQL connection string | `postgres://sherlock:sherlock@localhost:5432/sherlock?sslmode=disable` |
| `SHERLOCK_NATS_URL` | NATS server URL | `nats://localhost:4222` |
| `SHERLOCK_AUTO_MIGRATE` | Auto-apply DB migrations on startup | `false` |

### Slack

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_SLACK_BOT_TOKEN` | Slack Bot OAuth token | -- |
| `SHERLOCK_SLACK_APP_TOKEN` | Slack App-level token (Socket Mode) | -- |
| `SHERLOCK_SLACK_SIGNING_SECRET` | Slack signing secret (HTTP mode) | -- |
| `SHERLOCK_SLACK_MODE` | Transport: `socket`, `http`, or `both` | `socket` |

### Collectors

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_PROMETHEUS_URL` | Prometheus server URL | `http://localhost:9090` |
| `SHERLOCK_LOKI_URL` | Loki server URL | `http://localhost:3100` |
| `SHERLOCK_K8S_IN_CLUSTER` | Use in-cluster K8s config | `false` |
| `SHERLOCK_K8S_KUBECONFIG` | Path to kubeconfig file | -- |

### Alert Receivers

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_GRAFANA_HMAC_SECRET` | Grafana webhook HMAC secret | -- |
| `SHERLOCK_GITHUB_WEBHOOK_SECRET` | GitHub webhook secret | -- |
| `SHERLOCK_GITLAB_SECRET_TOKEN` | GitLab webhook secret token | -- |

### Deduplication

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_DEDUP_ENABLED` | Enable alert deduplication | `true` |
| `SHERLOCK_DEDUP_WINDOW` | Time window for duplicate detection | `1h` |

### Object Storage

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_S3_ENDPOINT` | S3-compatible endpoint | `http://localhost:9000` |
| `SHERLOCK_S3_BUCKET` | Bucket name | `sherlock` |
| `SHERLOCK_S3_ACCESS_KEY` | Access key | -- |
| `SHERLOCK_S3_SECRET_KEY` | Secret key | -- |

### LLM (Optional)

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_LLM_ENABLED` | Enable LLM-powered RCA | `false` |
| `SHERLOCK_LLM_PROVIDER` | Provider: `openai`, `vertex`, `anthropic`, `ollama` | `openai` |
| `SHERLOCK_LLM_MODEL` | Model name | `gpt-4o` |
| `SHERLOCK_LLM_API_KEY` | API key (not needed for Vertex/Ollama) | -- |
| `SHERLOCK_LLM_ENDPOINT` | Custom endpoint URL | -- |
| `SHERLOCK_LLM_GCP_PROJECT` | GCP project (Vertex AI only) | -- |
| `SHERLOCK_LLM_GCP_REGION` | GCP region (Vertex AI only) | -- |
| `SHERLOCK_LLM_TEMPERATURE` | Sampling temperature | `0.2` |
| `SHERLOCK_LLM_MAX_TOKENS` | Max response tokens | `4096` |
| `SHERLOCK_LLM_MAX_PASSES` | Max investigation passes | `3` |
| `SHERLOCK_LLM_TIMEOUT` | Per-call timeout | `120s` |

### Git Source Access (Optional)

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_GIT_ENABLED` | Enable Git source access and deploy collector | `false` |
| `SHERLOCK_GIT_PROVIDER` | Provider: `github` | `github` |
| `SHERLOCK_GIT_TOKEN` | GitHub personal access token | -- |
| `SHERLOCK_GIT_ORGANIZATION` | GitHub organization | -- |
| `SHERLOCK_GIT_DEFAULT_BRANCH` | Default branch to fetch from | `main` |

### Remediation Policies (Optional)

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_REMEDIATION_ENABLED` | Enable remediation policy engine | `true` |
| `SHERLOCK_REMEDIATION_POLICIES_PATH` | Path to custom policies YAML | -- (uses built-in defaults) |

### Observability (Optional)

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_TRACING_ENABLED` | Enable OpenTelemetry tracing | `false` |
| `SHERLOCK_TRACING_ENDPOINT` | OTLP HTTP exporter endpoint | `localhost:4318` |
| `SHERLOCK_TRACING_SAMPLE_RATE` | Trace sampling rate (0.0-1.0) | `1.0` |

### Security

| Variable | Description | Default |
|----------|-------------|---------|
| `SHERLOCK_API_KEY` | API key for `/api/v1/*` endpoints (empty = disabled) | -- |
| `SHERLOCK_RATE_LIMIT_RPS` | Webhook rate limit (requests/sec/IP) | `50` |
| `SHERLOCK_RATE_LIMIT_BURST` | Webhook rate limit burst capacity | `100` |

## Contributing

We welcome contributions! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

All contributions require a DCO sign-off. See the contributing guide for details.

## License

Apache License 2.0 -- see [LICENSE](LICENSE) for details.

## Reporting Vulnerabilities

To report security vulnerabilities, please see [SECURITY.md](SECURITY.md).

# Self-Hosted Deployment Guide

Deploy Sherlock to your Kubernetes cluster for production use.

## Prerequisites

- Kubernetes cluster (1.26+)
- Helm 3
- PostgreSQL instance (managed or self-hosted)
- S3-compatible object storage (AWS S3, MinIO, etc.)
- NATS with JetStream enabled
- Slack workspace with admin access

## Step 1: Create the Slack App

Follow the same process as the [Quick Start](quickstart.md#step-2-create-a-slack-app) to create a Slack app from the manifest.

For production, update the manifest's request URL to your actual ingress domain if using HTTP mode.

## Step 2: Prepare Secrets

Create a Kubernetes secret with your credentials:

```bash
kubectl create secret generic sherlock-secrets \
  --from-literal=postgres-dsn='postgres://user:pass@postgres:5432/sherlock?sslmode=require' \
  --from-literal=slack-bot-token='xoxb-...' \
  --from-literal=slack-app-token='xapp-...' \
  --from-literal=slack-signing-secret='...' \
  --from-literal=s3-access-key='...' \
  --from-literal=s3-secret-key='...'
```

## Step 3: Install with Helm

```bash
helm install sherlock charts/sherlock \
  --set postgres.dsn=valueFrom:secret \
  --set nats.url=nats://nats:4222 \
  --set objectstore.endpoint=https://s3.amazonaws.com \
  --set objectstore.bucket=sherlock-evidence \
  --set objectstore.region=us-east-1 \
  --set slack.mode=socket \
  --set collectors.prometheus.url=http://prometheus:9090 \
  --set collectors.loki.url=http://loki:3100 \
  --set collectors.kubernetes.inCluster=true
```

## Step 4: Configure RBAC

The Helm chart creates a ClusterRole with read-only access to:

- Pods, Pod logs, Events, Namespaces (core API)
- Deployments, ReplicaSets (apps API)

Review the ClusterRole and ClusterRoleBinding to ensure they meet your security requirements.

## Step 5: Connect Alert Sources

### Grafana Alerting

1. Go to **Alerting > Contact points**
2. Add a new contact point with type **Webhook**
3. Set the URL to `http://sherlock:8080/webhooks/grafana`
4. Optionally configure HMAC authentication

### Prometheus Alertmanager

Add to your `alertmanager.yml`:

```yaml
receivers:
  - name: sherlock
    webhook_configs:
      - url: http://sherlock:8080/webhooks/alertmanager
        send_resolved: true
```

## Scaling

Sherlock uses NATS JetStream for work distribution. You can run multiple replicas and investigations will be distributed across them. Increase `replicaCount` in your Helm values.

## Monitoring

Sherlock exposes a health endpoint at `/api/v1/health`. Configure your monitoring to check this endpoint.

## Upgrading

```bash
helm upgrade sherlock charts/sherlock --values your-values.yaml
```

Database migrations run automatically on startup.

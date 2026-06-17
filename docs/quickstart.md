# Quick Start Guide

Get Sherlock running locally in 5 minutes.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/)
- A Slack workspace where you can create apps

## Step 1: Clone the Repository

```bash
git clone https://github.com/rahulkosamkar/sherlock.git
cd sherlock
```

## Step 2: Create a Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps)
2. Click **Create New App** > **From a manifest**
3. Select your workspace
4. Paste the contents of `slack-app-manifest.yml` from this repo
5. Click **Create**

After creation, note these values from the app settings:

- **Bot User OAuth Token** (starts with `xoxb-`): found under **OAuth & Permissions**
- **App-Level Token** (starts with `xapp-`): create one under **Basic Information > App-Level Tokens** with the `connections:write` scope
- **Signing Secret**: found under **Basic Information > App Credentials**

## Step 3: Configure Environment

```bash
cp .env.example .env
```

Edit `.env` and fill in your Slack tokens:

```
SHERLOCK_SLACK_BOT_TOKEN=xoxb-your-actual-bot-token
SHERLOCK_SLACK_APP_TOKEN=xapp-your-actual-app-token
SHERLOCK_SLACK_SIGNING_SECRET=your-actual-signing-secret
```

The other values (Postgres, NATS, S3) are pre-configured for the Docker Compose setup.

## Step 4: Start Sherlock

```bash
make dev
```

This starts all services:
- **Postgres** on port 5432 (data storage)
- **MinIO** on port 9000/9001 (S3-compatible object storage)
- **NATS** on port 4222 (message queue with JetStream)
- **Sherlock** on port 8080 (the investigator)

## Step 5: Install the Bot in a Channel

1. Go to a Slack channel
2. Type `/invite @Sherlock`
3. The bot is now ready to investigate

## Step 6: Try It Out

### Via Slash Command

```
/investigate payments-api
```

### Via Message Shortcut

1. Right-click (or hover) on any alert message in Slack
2. Select **More actions** > **Investigate this alert**

### Via Webhook

Point your Grafana Alerting webhook contact point to:

```
http://localhost:8080/webhooks/grafana
```

Or your Alertmanager webhook to:

```
http://localhost:8080/webhooks/alertmanager
```

## Step 7: Verify

Check the health endpoint:

```bash
curl http://localhost:8080/api/v1/health
```

View investigation results via the API:

```bash
curl http://localhost:8080/api/v1/investigations/{investigation-id}
```

## Stopping

```bash
make dev-down
```

## Next Steps

- Read the [Self-Hosted Guide](self-hosted-guide.md) for production deployment
- Configure additional collectors (Prometheus, Loki, Kubernetes)
- Set up webhook receivers in your alerting tools

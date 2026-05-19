CREATE TABLE installations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL,
    workspace_id  TEXT NOT NULL UNIQUE,
    workspace_name TEXT NOT NULL,
    bot_token     TEXT NOT NULL,
    refresh_token TEXT,
    token_expiry  TIMESTAMPTZ,
    scopes        TEXT[],
    installed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    settings      JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE alerts (
    id           UUID PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    source       TEXT NOT NULL,
    status       TEXT NOT NULL,
    severity     TEXT NOT NULL,
    title        TEXT NOT NULL,
    summary      TEXT,
    fingerprint  TEXT NOT NULL,
    group_key    TEXT,
    starts_at    TIMESTAMPTZ NOT NULL,
    ends_at      TIMESTAMPTZ,
    labels       JSONB,
    annotations  JSONB,
    entity_hints JSONB,
    links        JSONB,
    raw_ref      TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_alerts_tenant_fingerprint ON alerts (tenant_id, fingerprint);

CREATE TABLE investigations (
    id               UUID PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    alert_ids        TEXT[],
    targets          JSONB,
    time_from        TIMESTAMPTZ NOT NULL,
    time_to          TIMESTAMPTZ NOT NULL,
    headline         TEXT,
    confidence       DOUBLE PRECISION NOT NULL DEFAULT 0,
    slack_channel_id TEXT,
    slack_thread_ts  TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);

CREATE INDEX idx_investigations_tenant_status ON investigations (tenant_id, status);

CREATE TABLE evidence (
    id               UUID PRIMARY KEY,
    investigation_id UUID NOT NULL REFERENCES investigations(id),
    kind             TEXT NOT NULL,
    source           TEXT NOT NULL,
    target           JSONB,
    collected_at     TIMESTAMPTZ NOT NULL,
    observed_at_from TIMESTAMPTZ,
    observed_at_to   TIMESTAMPTZ,
    summary          TEXT,
    body_ref         TEXT,
    query            TEXT,
    score            DOUBLE PRECISION NOT NULL DEFAULT 0,
    attributes       JSONB,
    redaction_state  TEXT NOT NULL DEFAULT 'none'
);

CREATE INDEX idx_evidence_investigation ON evidence (investigation_id);

CREATE TABLE timeline_events (
    id               UUID PRIMARY KEY,
    investigation_id UUID NOT NULL REFERENCES investigations(id),
    timestamp        TIMESTAMPTZ NOT NULL,
    kind             TEXT NOT NULL,
    source           TEXT NOT NULL,
    narrative        TEXT,
    evidence_ids     TEXT[],
    attributes       JSONB
);

CREATE INDEX idx_timeline_investigation_ts ON timeline_events (investigation_id, timestamp);

CREATE TABLE hypotheses (
    id               UUID PRIMARY KEY,
    investigation_id UUID NOT NULL REFERENCES investigations(id),
    title            TEXT NOT NULL,
    narrative        TEXT,
    cause_category   TEXT,
    confidence       DOUBLE PRECISION,
    supporting       TEXT[],
    contradicting    TEXT[],
    suggested_fixes  JSONB
);

CREATE INDEX idx_hypotheses_investigation ON hypotheses (investigation_id);

CREATE TABLE audit_log (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    actor     TEXT NOT NULL,
    action    TEXT NOT NULL,
    target    TEXT,
    metadata  JSONB NOT NULL DEFAULT '{}',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_tenant_ts ON audit_log (tenant_id, timestamp);

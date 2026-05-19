CREATE TABLE suppressions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fingerprint  TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_by   TEXT,
    reason       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_suppressions_fingerprint ON suppressions (fingerprint, expires_at);

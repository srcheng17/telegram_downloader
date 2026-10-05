BEGIN;

CREATE TABLE IF NOT EXISTS admin_account (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    credential_version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS admin_sessions (
    token_hash TEXT PRIMARY KEY,
    csrf_token TEXT NOT NULL,
    credential_version BIGINT NOT NULL,
    preauth BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires_at ON admin_sessions(expires_at);

CREATE TABLE IF NOT EXISTS source_settings (
    provider_id TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL,
    priority INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 1000),
    config JSONB NOT NULL DEFAULT '{}',
    auth_mode TEXT NOT NULL DEFAULT 'none',
    ciphertext BYTEA,
    nonce BYTEA,
    key_id TEXT,
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    credential_version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Reserved now for the Batch 2 rule editor; applied migrations stay immutable.
CREATE TABLE IF NOT EXISTS extraction_rules (
    singleton BOOLEAN PRIMARY KEY CHECK (singleton),
    rules_version BIGINT NOT NULL CHECK (rules_version > 0),
    definitions_version TEXT NOT NULL,
    rules JSONB NOT NULL CHECK (jsonb_typeof(rules) = 'array'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;

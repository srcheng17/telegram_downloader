BEGIN;

CREATE TABLE IF NOT EXISTS komga_connection (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    base_url TEXT NOT NULL,
    ciphertext BYTEA,
    nonce BYTEA,
    key_id TEXT,
    credential_version BIGINT NOT NULL DEFAULT 0,
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS komga_edit_operations (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    request_digest TEXT NOT NULL,
    book_id TEXT NOT NULL,
    library_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    source_sha256 TEXT NOT NULL,
    target_sha256 TEXT NOT NULL DEFAULT '',
    backup_ref TEXT NOT NULL DEFAULT '',
    desired_fields JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(desired_fields) = 'array'),
    prepared JSONB,
    state TEXT NOT NULL CHECK (state IN (
        'preparing', 'prepared', 'file_committed', 'sync_pending',
        'current_value_consistent', 'sync_failed', 'restore_needed', 'restored', 'aborted'
    )),
    file_committed BOOLEAN NOT NULL DEFAULT false,
    file_no_change BOOLEAN NOT NULL DEFAULT false,
    file_restored BOOLEAN NOT NULL DEFAULT false,
    projection_consistent BOOLEAN NOT NULL DEFAULT false,
    analyze_verified BOOLEAN NOT NULL DEFAULT false,
    last_error_code TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_komga_edit_operations_book_created
    ON komga_edit_operations(book_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_komga_edit_operations_state
    ON komga_edit_operations(state, updated_at)
    WHERE state IN ('preparing', 'prepared', 'file_committed', 'sync_pending', 'sync_failed', 'restore_needed');

CREATE UNIQUE INDEX IF NOT EXISTS idx_komga_edit_operations_active_path
    ON komga_edit_operations(library_id, relative_path)
    WHERE state IN ('preparing', 'prepared', 'file_committed', 'sync_pending', 'sync_failed', 'restore_needed');

COMMIT;

BEGIN;

ALTER TABLE task_core_inputs ADD COLUMN IF NOT EXISTS source_sha256 TEXT;

-- Created in the same transaction as task/input/event. Receipts survive API
-- restarts and never contain private metadata or file contents.
CREATE TABLE IF NOT EXISTS task_core_submissions (
    idempotency_key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    task_id UUID NOT NULL REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    needs_confirmation BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    CHECK (request_hash ~ '^[0-9a-f]{64}$')
);
CREATE INDEX IF NOT EXISTS idx_task_core_submissions_task ON task_core_submissions(task_id);

COMMIT;

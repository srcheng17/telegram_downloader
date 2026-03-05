BEGIN;

ALTER TABLE tasks
	ADD COLUMN IF NOT EXISTS enqueue_token TEXT,
	ADD COLUMN IF NOT EXISTS claimed_by TEXT,
	ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS cancel_requested_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS end_time DOUBLE PRECISION;

CREATE INDEX IF NOT EXISTS idx_tasks_status_start_time ON tasks (status, start_time DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_canonical_url_start_time ON tasks (canonical_url, start_time DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_heartbeat ON tasks (status, heartbeat_at);

COMMIT;

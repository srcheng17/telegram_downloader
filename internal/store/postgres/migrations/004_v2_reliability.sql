BEGIN;

ALTER TABLE v2_tasks
	ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS cancel_requested_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_v2_tasks_status_heartbeat
	ON v2_tasks (status, heartbeat_at);

COMMIT;

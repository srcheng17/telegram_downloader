BEGIN;

CREATE TABLE IF NOT EXISTS v2_tasks (
	id TEXT PRIMARY KEY,
	url TEXT NOT NULL,
	canonical_url TEXT NOT NULL,
	status TEXT NOT NULL,
	enqueue_token TEXT NOT NULL,
	error TEXT,
	result_zip_path TEXT,
	claimed_by TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_v2_tasks_status_created_at
	ON v2_tasks (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_v2_tasks_canonical_url_created_at
	ON v2_tasks (canonical_url, created_at DESC);

CREATE TABLE IF NOT EXISTS v2_task_events (
	id BIGSERIAL PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES v2_tasks(id) ON DELETE CASCADE,
	event_type TEXT NOT NULL,
	from_status TEXT,
	to_status TEXT,
	payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_v2_task_events_task_created_at
	ON v2_task_events (task_id, created_at DESC);

COMMIT;

BEGIN;

CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	url TEXT NOT NULL,
	canonical_url TEXT,
	status TEXT NOT NULL,
	start_time DOUBLE PRECISION NOT NULL,
	error TEXT,
	progress INTEGER NOT NULL DEFAULT 0,
	total_images INTEGER NOT NULL DEFAULT 0,
	image_concurrency INTEGER NOT NULL DEFAULT 2,
	result_zip_path TEXT,
	author TEXT,
	series_name TEXT,
	comic_name TEXT,
	summary TEXT,
	tags_raw TEXT,
	tags_normalized TEXT,
	genres_raw TEXT,
	genres_normalized TEXT,
	enqueue_token TEXT,
	claimed_by TEXT,
	claimed_at TIMESTAMPTZ,
	heartbeat_at TIMESTAMPTZ,
	cancel_requested_at TIMESTAMPTZ,
	retry_count INTEGER NOT NULL DEFAULT 0,
	version BIGINT NOT NULL DEFAULT 0,
	end_time DOUBLE PRECISION
);

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

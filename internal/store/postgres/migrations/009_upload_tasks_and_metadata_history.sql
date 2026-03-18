BEGIN;

ALTER TABLE v2_tasks
	ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT 'url',
	ADD COLUMN IF NOT EXISTS source_archive_path TEXT,
	ADD COLUMN IF NOT EXISTS source_archive_name TEXT,
	ADD COLUMN IF NOT EXISTS upload_loaded_bytes BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS upload_total_bytes BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS retryable BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS metadata_history (
	id BIGSERIAL PRIMARY KEY,
	task_type TEXT NOT NULL,
	url TEXT,
	author TEXT,
	series_name TEXT,
	comic_name TEXT,
	summary TEXT,
	tags TEXT,
	genres TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_metadata_history_created_at
	ON metadata_history (created_at DESC);

COMMIT;

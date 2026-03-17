CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_tasks_url_trgm
	ON tasks
	USING gin (url gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_tasks_canonical_url_trgm
	ON tasks
	USING gin (canonical_url gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_v2_tasks_url_trgm
	ON v2_tasks
	USING gin (url gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_v2_tasks_canonical_url_trgm
	ON v2_tasks
	USING gin (canonical_url gin_trgm_ops);

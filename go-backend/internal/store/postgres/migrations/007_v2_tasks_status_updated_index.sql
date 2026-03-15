CREATE INDEX IF NOT EXISTS idx_v2_tasks_status_updated_at ON v2_tasks (status, updated_at DESC);

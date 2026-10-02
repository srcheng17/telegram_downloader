BEGIN;

ALTER TABLE task_core_tasks ADD COLUMN IF NOT EXISTS generation BIGINT NOT NULL DEFAULT 0;
UPDATE task_core_tasks SET generation = attempt WHERE generation = 0 AND attempt > 0;
ALTER TABLE task_core_inputs ADD COLUMN IF NOT EXISTS runtime_settings JSONB;
CREATE INDEX IF NOT EXISTS idx_task_core_tasks_created_id ON task_core_tasks (created_at DESC, id DESC);

COMMIT;

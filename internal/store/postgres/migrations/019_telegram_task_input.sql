BEGIN;
ALTER TABLE task_core_tasks DROP CONSTRAINT IF EXISTS task_core_tasks_kind_check;
ALTER TABLE task_core_tasks ADD CONSTRAINT task_core_tasks_kind_check CHECK (kind IN ('url','upload','telegram'));
ALTER TABLE task_core_inputs ADD COLUMN IF NOT EXISTS telegram_source JSONB;
COMMIT;

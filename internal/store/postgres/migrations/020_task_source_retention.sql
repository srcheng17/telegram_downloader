BEGIN;
ALTER TABLE task_core_results ADD COLUMN IF NOT EXISTS retention_manifest JSONB;
COMMIT;

BEGIN;

CREATE TABLE IF NOT EXISTS metadata_definition_versions (
    version TEXT PRIMARY KEY,
    registry JSONB NOT NULL CHECK (jsonb_typeof(registry) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS metadata_definition_head (
    singleton BOOLEAN PRIMARY KEY CHECK (singleton),
    version TEXT NOT NULL REFERENCES metadata_definition_versions(version)
);

ALTER TABLE task_core_inputs ADD COLUMN IF NOT EXISTS metadata_document JSONB
    CHECK (metadata_document IS NULL OR jsonb_typeof(metadata_document) = 'object');
ALTER TABLE metadata_history ADD COLUMN IF NOT EXISTS metadata_document JSONB
    CHECK (metadata_document IS NULL OR jsonb_typeof(metadata_document) = 'object');
ALTER TABLE metadata_history ADD COLUMN IF NOT EXISTS task_id TEXT;
CREATE INDEX IF NOT EXISTS idx_metadata_history_task_id ON metadata_history(task_id);

ALTER TABLE task_core_results ADD COLUMN IF NOT EXISTS effective_metadata_document JSONB
    CHECK (effective_metadata_document IS NULL OR jsonb_typeof(effective_metadata_document) = 'object');
ALTER TABLE task_core_results ADD COLUMN IF NOT EXISTS generation BIGINT;

COMMIT;

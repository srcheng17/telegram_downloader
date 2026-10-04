BEGIN;
CREATE TABLE IF NOT EXISTS task_core_retention (
    task_id UUID NOT NULL REFERENCES task_core_tasks(id),
    generation BIGINT NOT NULL CHECK(generation > 0),
    manifest JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(task_id,generation)
);
COMMIT;

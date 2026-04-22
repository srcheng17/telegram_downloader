BEGIN;

CREATE TABLE IF NOT EXISTS task_core_tasks (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('url', 'upload')),
    status TEXT NOT NULL CHECK (status IN ('CREATED', 'READY', 'RUNNING', 'CANCELING', 'SUCCEEDED', 'FAILED', 'CANCELED')),
    attempt INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ready_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    cancel_requested_at TIMESTAMPTZ,
    last_error TEXT,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_task_core_tasks_status_ready_at
    ON task_core_tasks (status, ready_at ASC NULLS LAST, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_task_core_tasks_running_lease
    ON task_core_tasks (status, lease_expires_at)
    WHERE status = 'RUNNING';

CREATE INDEX IF NOT EXISTS idx_task_core_tasks_canceling_updated
    ON task_core_tasks (status, updated_at)
    WHERE status = 'CANCELING';

CREATE TABLE IF NOT EXISTS task_core_inputs (
    task_id UUID PRIMARY KEY REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    url TEXT,
    canonical_url TEXT,
    source_archive_name TEXT,
    source_archive_path TEXT,
    source_archive_size BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_task_core_inputs_canonical_url
    ON task_core_inputs (canonical_url);

CREATE TABLE IF NOT EXISTS task_core_progress (
    task_id UUID PRIMARY KEY REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    phase TEXT NOT NULL,
    current BIGINT NOT NULL DEFAULT 0,
    total BIGINT NOT NULL DEFAULT 0,
    unit TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS task_core_results (
    task_id UUID PRIMARY KEY REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    artifact_path TEXT NOT NULL,
    artifact_name TEXT NOT NULL,
    artifact_size BIGINT NOT NULL,
    artifact_kind TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    komga_copied_at TIMESTAMPTZ,
    komga_target_path TEXT
);

CREATE TABLE IF NOT EXISTS task_core_events (
    id BIGSERIAL PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    from_status TEXT,
    to_status TEXT,
    actor TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_core_events_task_created_at
    ON task_core_events (task_id, created_at DESC);

COMMIT;

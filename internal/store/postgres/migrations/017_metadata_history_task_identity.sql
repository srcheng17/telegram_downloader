BEGIN;

-- Task snapshots are distinct even when their legacy seven-field projections match.
-- Existing unlinked history keeps the old deduplication behavior.
DROP INDEX IF EXISTS idx_metadata_history_unique_entry;
CREATE UNIQUE INDEX IF NOT EXISTS idx_metadata_history_unique_entry
ON metadata_history ((
    md5(
        LENGTH(LOWER(BTRIM(task_type)))::TEXT || ':' || LOWER(BTRIM(task_type)) || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(url), ''))::TEXT || ':' || COALESCE(BTRIM(url), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(author), ''))::TEXT || ':' || COALESCE(BTRIM(author), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(series_name), ''))::TEXT || ':' || COALESCE(BTRIM(series_name), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(series_number), ''))::TEXT || ':' || COALESCE(BTRIM(series_number), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(comic_name), ''))::TEXT || ':' || COALESCE(BTRIM(comic_name), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(summary), ''))::TEXT || ':' || COALESCE(BTRIM(summary), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(tags), ''))::TEXT || ':' || COALESCE(BTRIM(tags), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(genres), ''))::TEXT || ':' || COALESCE(BTRIM(genres), '')
    )
)) WHERE task_id IS NULL;

DROP INDEX IF EXISTS idx_metadata_history_task_id;
CREATE UNIQUE INDEX idx_metadata_history_task_id
    ON metadata_history(task_id) WHERE task_id IS NOT NULL;

COMMIT;

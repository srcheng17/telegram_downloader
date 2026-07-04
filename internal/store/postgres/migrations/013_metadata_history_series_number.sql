BEGIN;

ALTER TABLE metadata_history
    ADD COLUMN IF NOT EXISTS series_number TEXT;

UPDATE metadata_history mh
SET series_number = NULLIF(BTRIM(i.metadata->>'series_number'), '')
FROM task_core_tasks t
JOIN task_core_inputs i ON i.task_id = t.id
WHERE mh.series_number IS NULL
    AND LOWER(BTRIM(mh.task_type)) = LOWER(BTRIM(t.kind))
    AND COALESCE(BTRIM(mh.url), '') = CASE
        WHEN t.kind = 'url' THEN COALESCE(BTRIM(i.url), '')
        ELSE ''
    END
    AND COALESCE(BTRIM(mh.author), '') = COALESCE(BTRIM(i.metadata->>'author'), '')
    AND COALESCE(BTRIM(mh.series_name), '') = COALESCE(BTRIM(i.metadata->>'series_name'), '')
    AND COALESCE(BTRIM(mh.comic_name), '') = COALESCE(BTRIM(i.metadata->>'comic_name'), '')
    AND COALESCE(BTRIM(mh.summary), '') = COALESCE(BTRIM(i.metadata->>'summary'), '')
    AND COALESCE(BTRIM(mh.tags), '') = COALESCE(BTRIM(i.metadata->>'tags'), '')
    AND COALESCE(BTRIM(mh.genres), '') = COALESCE(BTRIM(i.metadata->>'genres'), '')
    AND NULLIF(BTRIM(i.metadata->>'series_number'), '') IS NOT NULL;

DROP INDEX IF EXISTS idx_metadata_history_unique_entry;

WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY md5(
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
            ORDER BY created_at DESC, id DESC
        ) AS duplicate_rank
    FROM metadata_history
)
DELETE FROM metadata_history
USING ranked
WHERE metadata_history.id = ranked.id
    AND ranked.duplicate_rank > 1;

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
));

COMMIT;

BEGIN;

WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY md5(
                LENGTH(LOWER(BTRIM(task_type)))::TEXT || ':' || LOWER(BTRIM(task_type)) || E'\x1f' ||
                LENGTH(COALESCE(BTRIM(url), ''))::TEXT || ':' || COALESCE(BTRIM(url), '') || E'\x1f' ||
                LENGTH(COALESCE(BTRIM(author), ''))::TEXT || ':' || COALESCE(BTRIM(author), '') || E'\x1f' ||
                LENGTH(COALESCE(BTRIM(series_name), ''))::TEXT || ':' || COALESCE(BTRIM(series_name), '') || E'\x1f' ||
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
        LENGTH(COALESCE(BTRIM(comic_name), ''))::TEXT || ':' || COALESCE(BTRIM(comic_name), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(summary), ''))::TEXT || ':' || COALESCE(BTRIM(summary), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(tags), ''))::TEXT || ':' || COALESCE(BTRIM(tags), '') || E'\x1f' ||
        LENGTH(COALESCE(BTRIM(genres), ''))::TEXT || ':' || COALESCE(BTRIM(genres), '')
    )
));

WITH candidates AS (
    SELECT
        t.kind AS task_type,
        CASE
            WHEN t.kind = 'url' THEN NULLIF(BTRIM(i.url), '')
            ELSE NULL
        END AS url,
        NULLIF(BTRIM(i.metadata->>'author'), '') AS author,
        NULLIF(BTRIM(i.metadata->>'series_name'), '') AS series_name,
        NULLIF(BTRIM(i.metadata->>'comic_name'), '') AS comic_name,
        NULLIF(BTRIM(i.metadata->>'summary'), '') AS summary,
        NULLIF(BTRIM(i.metadata->>'tags'), '') AS tags,
        NULLIF(BTRIM(i.metadata->>'genres'), '') AS genres,
        t.created_at AS created_at
    FROM task_core_tasks t
    JOIN task_core_inputs i ON i.task_id = t.id
    WHERE NULLIF(BTRIM(i.metadata->>'author'), '') IS NOT NULL
        OR NULLIF(BTRIM(i.metadata->>'series_name'), '') IS NOT NULL
        OR NULLIF(BTRIM(i.metadata->>'comic_name'), '') IS NOT NULL
        OR NULLIF(BTRIM(i.metadata->>'summary'), '') IS NOT NULL
        OR NULLIF(BTRIM(i.metadata->>'tags'), '') IS NOT NULL
        OR NULLIF(BTRIM(i.metadata->>'genres'), '') IS NOT NULL
),
deduped AS (
    SELECT DISTINCT ON (
        LOWER(BTRIM(task_type)),
        COALESCE(BTRIM(url), ''),
        COALESCE(BTRIM(author), ''),
        COALESCE(BTRIM(series_name), ''),
        COALESCE(BTRIM(comic_name), ''),
        COALESCE(BTRIM(summary), ''),
        COALESCE(BTRIM(tags), ''),
        COALESCE(BTRIM(genres), '')
    )
        task_type,
        url,
        author,
        series_name,
        comic_name,
        summary,
        tags,
        genres,
        created_at
    FROM candidates
    ORDER BY
        LOWER(BTRIM(task_type)),
        COALESCE(BTRIM(url), ''),
        COALESCE(BTRIM(author), ''),
        COALESCE(BTRIM(series_name), ''),
        COALESCE(BTRIM(comic_name), ''),
        COALESCE(BTRIM(summary), ''),
        COALESCE(BTRIM(tags), ''),
        COALESCE(BTRIM(genres), ''),
        created_at DESC
)
INSERT INTO metadata_history (
    task_type,
    url,
    author,
    series_name,
    comic_name,
    summary,
    tags,
    genres,
    created_at
)
SELECT
    task_type,
    url,
    author,
    series_name,
    comic_name,
    summary,
    tags,
    genres,
    created_at
FROM deduped
ON CONFLICT DO NOTHING;

COMMIT;

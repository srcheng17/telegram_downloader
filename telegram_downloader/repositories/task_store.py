import os
import sqlite3
import threading


class TaskStore:
    _TASK_SELECT_FIELDS = """
        id,
        url,
        canonical_url,
        status,
        start_time,
        error,
        progress,
        total_images,
        image_concurrency,
        result_zip_path,
        author,
        series_name,
        comic_name,
        summary,
        tags_raw,
        tags_normalized
    """
    _COLUMNS = {
        "id",
        "url",
        "canonical_url",
        "status",
        "start_time",
        "error",
        "progress",
        "total_images",
        "image_concurrency",
        "result_zip_path",
        "author",
        "series_name",
        "comic_name",
        "summary",
        "tags_raw",
        "tags_normalized",
    }

    def __init__(self, db_path):
        self.db_path = db_path
        self._lock = threading.RLock()
        self._ensure_db()

    def _connect(self):
        connection = sqlite3.connect(self.db_path, timeout=30, check_same_thread=False)
        connection.row_factory = sqlite3.Row
        return connection

    def _ensure_db(self):
        db_dir = os.path.dirname(self.db_path)
        if db_dir:
            os.makedirs(db_dir, exist_ok=True)
        with self._lock, self._connect() as conn:
            conn.execute("PRAGMA journal_mode=WAL")
            conn.execute("PRAGMA synchronous=NORMAL")
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS tasks (
                    id TEXT PRIMARY KEY,
                    url TEXT NOT NULL,
                    canonical_url TEXT,
                    status TEXT NOT NULL,
                    start_time REAL NOT NULL,
                    error TEXT,
                    progress INTEGER NOT NULL DEFAULT 0,
                    total_images INTEGER NOT NULL DEFAULT 0,
                    image_concurrency INTEGER NOT NULL DEFAULT 2,
                    result_zip_path TEXT,
                    author TEXT,
                    series_name TEXT,
                    comic_name TEXT,
                    summary TEXT,
                    tags_raw TEXT,
                    tags_normalized TEXT
                )
                """
            )
            self._ensure_column(conn, "tasks", "canonical_url", "TEXT")
            self._ensure_column(conn, "tasks", "result_zip_path", "TEXT")
            self._ensure_column(conn, "tasks", "author", "TEXT")
            self._ensure_column(conn, "tasks", "series_name", "TEXT")
            self._ensure_column(conn, "tasks", "comic_name", "TEXT")
            self._ensure_column(conn, "tasks", "summary", "TEXT")
            self._ensure_column(conn, "tasks", "tags_raw", "TEXT")
            self._ensure_column(conn, "tasks", "tags_normalized", "TEXT")
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_tasks_start_time ON tasks(start_time DESC)"
            )
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)"
            )
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_tasks_canonical_url_start_time "
                "ON tasks(canonical_url, start_time DESC)"
            )

    def _ensure_column(self, conn, table_name, column_name, column_sql):
        existing_columns = {
            row["name"] for row in conn.execute(f"PRAGMA table_info({table_name})").fetchall()
        }
        if column_name in existing_columns:
            return
        conn.execute(f"ALTER TABLE {table_name} ADD COLUMN {column_name} {column_sql}")

    def _normalize_task_payload(self, task):
        url = task["url"]
        normalized_url = url.strip()
        raw_canonical_url = task.get("canonical_url")
        canonical_url = (raw_canonical_url or "").strip() if raw_canonical_url is not None else ""
        if not canonical_url:
            canonical_url = normalized_url
        return {
            "id": task["id"],
            "url": url,
            "canonical_url": canonical_url,
            "status": task["status"],
            "start_time": float(task["start_time"]),
            "error": task.get("error"),
            "progress": int(task.get("progress", 0)),
            "total_images": int(task.get("total_images", 0)),
            "image_concurrency": int(task.get("image_concurrency", 2)),
            "result_zip_path": task.get("result_zip_path"),
            "author": task.get("author"),
            "series_name": task.get("series_name"),
            "comic_name": task.get("comic_name"),
            "summary": task.get("summary"),
            "tags_raw": task.get("tags_raw"),
            "tags_normalized": task.get("tags_normalized"),
        }

    def create_task(self, task):
        payload = self._normalize_task_payload(task)
        with self._lock, self._connect() as conn:
            conn.execute(
                """
                INSERT INTO tasks (
                    id, url, canonical_url, status, start_time, error, progress, total_images, image_concurrency, result_zip_path, author, series_name, comic_name, summary, tags_raw, tags_normalized
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(id) DO UPDATE SET
                    url=excluded.url,
                    canonical_url=excluded.canonical_url,
                    status=excluded.status,
                    start_time=excluded.start_time,
                    error=excluded.error,
                    progress=excluded.progress,
                    total_images=excluded.total_images,
                    image_concurrency=excluded.image_concurrency,
                    result_zip_path=excluded.result_zip_path,
                    author=excluded.author,
                    series_name=excluded.series_name,
                    comic_name=excluded.comic_name,
                    summary=excluded.summary,
                    tags_raw=excluded.tags_raw,
                    tags_normalized=excluded.tags_normalized
                """,
                (
                    payload["id"],
                    payload["url"],
                    payload["canonical_url"],
                    payload["status"],
                    payload["start_time"],
                    payload["error"],
                    payload["progress"],
                    payload["total_images"],
                    payload["image_concurrency"],
                    payload["result_zip_path"],
                    payload["author"],
                    payload["series_name"],
                    payload["comic_name"],
                    payload["summary"],
                    payload["tags_raw"],
                    payload["tags_normalized"],
                ),
            )

    def claim_download_task(self, task, active_statuses, reuse_success=True):
        """Atomically reuse an existing task or create a new pending task."""
        payload = self._normalize_task_payload(task)
        canonical_url = payload["canonical_url"].strip()
        if not canonical_url:
            canonical_url = payload["url"].strip()
            payload["canonical_url"] = canonical_url

        normalized_statuses = [status for status in (active_statuses or []) if status]
        with self._lock, self._connect() as conn:
            conn.execute("BEGIN IMMEDIATE")

            if reuse_success:
                success_row = conn.execute(
                    f"""
                    SELECT {self._TASK_SELECT_FIELDS}
                    FROM tasks
                    WHERE
                        status = 'SUCCESS'
                        AND result_zip_path IS NOT NULL
                        AND result_zip_path != ''
                        AND (
                            canonical_url = ?
                            OR (canonical_url IS NULL AND url = ?)
                        )
                    ORDER BY start_time DESC
                    LIMIT 1
                    """,
                    (canonical_url, canonical_url),
                ).fetchone()
                if success_row is not None:
                    return {"decision": "reuse_success", "task": dict(success_row)}

            if normalized_statuses:
                placeholders = ", ".join(["?"] * len(normalized_statuses))
                active_row = conn.execute(
                    f"""
                    SELECT {self._TASK_SELECT_FIELDS}
                    FROM tasks
                    WHERE
                        status IN ({placeholders})
                        AND (
                            canonical_url = ?
                            OR (canonical_url IS NULL AND url = ?)
                        )
                    ORDER BY start_time DESC
                    LIMIT 1
                    """,
                    [*normalized_statuses, canonical_url, canonical_url],
                ).fetchone()
                if active_row is not None:
                    return {"decision": "reuse_active", "task": dict(active_row)}

            conn.execute(
                """
                INSERT INTO tasks (
                    id,
                    url,
                    canonical_url,
                    status,
                    start_time,
                    error,
                    progress,
                    total_images,
                    image_concurrency,
                    result_zip_path,
                    author,
                    series_name,
                    comic_name,
                    summary,
                    tags_raw,
                    tags_normalized
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    payload["id"],
                    payload["url"],
                    payload["canonical_url"],
                    payload["status"],
                    payload["start_time"],
                    payload["error"],
                    payload["progress"],
                    payload["total_images"],
                    payload["image_concurrency"],
                    payload["result_zip_path"],
                    payload["author"],
                    payload["series_name"],
                    payload["comic_name"],
                    payload["summary"],
                    payload["tags_raw"],
                    payload["tags_normalized"],
                ),
            )
            created_row = conn.execute(
                f"""
                SELECT {self._TASK_SELECT_FIELDS}
                FROM tasks
                WHERE id = ?
                """,
                (payload["id"],),
            ).fetchone()
            if created_row is None:
                raise RuntimeError("Failed to create claimed download task.")
            return {"decision": "created", "task": dict(created_row)}

    def update_task(self, task_id, **fields):
        updates = {k: v for k, v in fields.items() if k in self._COLUMNS and k != "id"}
        if not updates:
            return
        clauses = []
        values = []
        for key, value in updates.items():
            clauses.append(f"{key} = ?")
            values.append(value)
        values.append(task_id)
        with self._lock, self._connect() as conn:
            conn.execute(
                f"UPDATE tasks SET {', '.join(clauses)} WHERE id = ?",
                values,
            )

    def increment_progress(self, task_id, step=1):
        with self._lock, self._connect() as conn:
            conn.execute(
                "UPDATE tasks SET progress = progress + ? WHERE id = ?",
                (int(step), task_id),
            )

    def mark_in_progress(self, task_id):
        with self._lock, self._connect() as conn:
            cursor = conn.execute(
                "UPDATE tasks SET status = ? WHERE id = ? AND status = ?",
                ("IN_PROGRESS", task_id, "PENDING"),
            )
            return cursor.rowcount > 0

    def get_field(self, task_id, field, default=None):
        if field not in self._COLUMNS:
            return default
        with self._lock, self._connect() as conn:
            row = conn.execute(
                f"SELECT {field} FROM tasks WHERE id = ?",
                (task_id,),
            ).fetchone()
        if row is None:
            return default
        value = row[field]
        return default if value is None else value

    def list_paginated(self, page, per_page, status=None, keyword=None):
        safe_page = max(1, int(page))
        safe_per_page = max(1, int(per_page))
        normalized_status = (status or "").strip().upper()
        normalized_keyword = (keyword or "").strip()

        where_clauses = []
        where_values = []
        if normalized_status:
            where_clauses.append("status = ?")
            where_values.append(normalized_status)
        if normalized_keyword:
            query_like = f"%{normalized_keyword}%"
            where_clauses.append(
                "(id LIKE ? OR url LIKE ? OR canonical_url LIKE ? OR error LIKE ?)"
            )
            where_values.extend([query_like, query_like, query_like, query_like])
        where_sql = f"WHERE {' AND '.join(where_clauses)}" if where_clauses else ""

        with self._lock, self._connect() as conn:
            total = conn.execute(
                f"SELECT COUNT(*) AS count FROM tasks {where_sql}",
                where_values,
            ).fetchone()["count"]
            total_pages = max(1, (total + safe_per_page - 1) // safe_per_page)
            safe_page = min(safe_page, total_pages)
            offset = (safe_page - 1) * safe_per_page
            rows = conn.execute(
                f"""
                SELECT
                    id,
                    url,
                    canonical_url,
                    status,
                    start_time,
                    error,
                    progress,
                    total_images,
                    image_concurrency,
                    result_zip_path,
                    author,
                    series_name,
                    comic_name,
                    summary,
                    tags_raw,
                    tags_normalized
                FROM tasks
                {where_sql}
                ORDER BY start_time DESC
                LIMIT ? OFFSET ?
                """,
                [*where_values, safe_per_page, offset],
            ).fetchall()

        return [dict(row) for row in rows], total, total_pages, safe_page

    def get_status_counts(self):
        with self._lock, self._connect() as conn:
            rows = conn.execute(
                """
                SELECT status, COUNT(*) AS count
                FROM tasks
                GROUP BY status
                """
            ).fetchall()
        return {row["status"]: int(row["count"]) for row in rows}

    def get_task(self, task_id):
        with self._lock, self._connect() as conn:
            row = conn.execute(
                f"""
                SELECT {self._TASK_SELECT_FIELDS}
                FROM tasks
                WHERE id = ?
                """,
                (task_id,),
            ).fetchone()
        if row is None:
            return None
        return dict(row)

    def find_latest_success_by_canonical_url(self, canonical_url):
        normalized_url = (canonical_url or "").strip()
        if not normalized_url:
            return None

        with self._lock, self._connect() as conn:
            row = conn.execute(
                f"""
                SELECT {self._TASK_SELECT_FIELDS}
                FROM tasks
                WHERE
                    status = 'SUCCESS'
                    AND result_zip_path IS NOT NULL
                    AND result_zip_path != ''
                    AND (
                        canonical_url = ?
                        OR (canonical_url IS NULL AND url = ?)
                    )
                ORDER BY start_time DESC
                LIMIT 1
                """,
                (normalized_url, normalized_url),
            ).fetchone()

        if row is None:
            return None
        return dict(row)

    def find_latest_active_by_canonical_url(self, canonical_url, statuses):
        normalized_url = (canonical_url or "").strip()
        normalized_statuses = [status for status in (statuses or []) if status]
        if not normalized_url or not normalized_statuses:
            return None

        placeholders = ", ".join(["?"] * len(normalized_statuses))
        with self._lock, self._connect() as conn:
            row = conn.execute(
                f"""
                SELECT {self._TASK_SELECT_FIELDS}
                FROM tasks
                WHERE
                    status IN ({placeholders})
                    AND (
                        canonical_url = ?
                        OR (canonical_url IS NULL AND url = ?)
                    )
                ORDER BY start_time DESC
                LIMIT 1
                """,
                [*normalized_statuses, normalized_url, normalized_url],
            ).fetchone()

        if row is None:
            return None
        return dict(row)

    def list_tasks_older_than(self, cutoff_start_time, exclude_statuses=None):
        normalized_exclusions = [status for status in (exclude_statuses or []) if status]
        query = """
            SELECT
                id,
                start_time,
                status,
                result_zip_path
            FROM tasks
            WHERE start_time < ?
        """
        values = [float(cutoff_start_time)]
        if normalized_exclusions:
            placeholders = ", ".join(["?"] * len(normalized_exclusions))
            query += f" AND status NOT IN ({placeholders})"
            values.extend(normalized_exclusions)

        with self._lock, self._connect() as conn:
            rows = conn.execute(query, values).fetchall()
        return [dict(row) for row in rows]

    def list_tasks_with_statuses_older_than(self, cutoff_start_time, statuses):
        normalized_statuses = [status for status in (statuses or []) if status]
        if not normalized_statuses:
            return []
        placeholders = ", ".join(["?"] * len(normalized_statuses))
        with self._lock, self._connect() as conn:
            rows = conn.execute(
                f"""
                SELECT
                    id,
                    start_time,
                    status,
                    result_zip_path
                FROM tasks
                WHERE start_time < ? AND status IN ({placeholders})
                """,
                [float(cutoff_start_time), *normalized_statuses],
            ).fetchall()
        return [dict(row) for row in rows]

    def delete_tasks_by_ids(self, task_ids):
        normalized_ids = [task_id for task_id in (task_ids or []) if task_id]
        if not normalized_ids:
            return 0
        placeholders = ", ".join(["?"] * len(normalized_ids))
        with self._lock, self._connect() as conn:
            cursor = conn.execute(
                f"DELETE FROM tasks WHERE id IN ({placeholders})",
                normalized_ids,
            )
            return cursor.rowcount

    def has_active_tasks(self, statuses):
        normalized_statuses = [status for status in statuses if status]
        if not normalized_statuses:
            return False
        placeholders = ", ".join(["?"] * len(normalized_statuses))
        with self._lock, self._connect() as conn:
            row = conn.execute(
                f"SELECT 1 FROM tasks WHERE status IN ({placeholders}) LIMIT 1",
                normalized_statuses,
            ).fetchone()
        return row is not None

    def list_tasks_with_result_zip_older_than(self, cutoff_start_time, exclude_statuses=None):
        normalized_exclusions = [status for status in (exclude_statuses or []) if status]
        query = """
            SELECT id, result_zip_path
            FROM tasks
            WHERE start_time < ? AND result_zip_path IS NOT NULL AND result_zip_path != ''
        """
        values = [float(cutoff_start_time)]
        if normalized_exclusions:
            placeholders = ", ".join(["?"] * len(normalized_exclusions))
            query += f" AND status NOT IN ({placeholders})"
            values.extend(normalized_exclusions)

        with self._lock, self._connect() as conn:
            rows = conn.execute(query, values).fetchall()
        return [dict(row) for row in rows]

    def list_result_zip_paths(self):
        with self._lock, self._connect() as conn:
            rows = conn.execute(
                """
                SELECT DISTINCT result_zip_path
                FROM tasks
                WHERE result_zip_path IS NOT NULL AND result_zip_path != ''
                """
            ).fetchall()
        return [row["result_zip_path"] for row in rows]

    def clear_result_zip_path(self, task_id):
        with self._lock, self._connect() as conn:
            conn.execute(
                "UPDATE tasks SET result_zip_path = NULL WHERE id = ?",
                (task_id,),
            )

    def delete_older_than(self, cutoff_start_time, exclude_statuses=None):
        normalized_exclusions = [status for status in (exclude_statuses or []) if status]
        query = "DELETE FROM tasks WHERE start_time < ?"
        values = [float(cutoff_start_time)]
        if normalized_exclusions:
            placeholders = ", ".join(["?"] * len(normalized_exclusions))
            query += f" AND status NOT IN ({placeholders})"
            values.extend(normalized_exclusions)

        with self._lock, self._connect() as conn:
            cursor = conn.execute(query, values)
            return cursor.rowcount

    def clear(self):
        with self._lock, self._connect() as conn:
            conn.execute("DELETE FROM tasks")

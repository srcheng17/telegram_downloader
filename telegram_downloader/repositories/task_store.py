import os
import sqlite3
import threading


class TaskStore:
    _COLUMNS = {
        "id",
        "url",
        "status",
        "start_time",
        "error",
        "progress",
        "total_images",
        "image_concurrency",
        "result_zip_path",
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
                    status TEXT NOT NULL,
                    start_time REAL NOT NULL,
                    error TEXT,
                    progress INTEGER NOT NULL DEFAULT 0,
                    total_images INTEGER NOT NULL DEFAULT 0,
                    image_concurrency INTEGER NOT NULL DEFAULT 2,
                    result_zip_path TEXT
                )
                """
            )
            self._ensure_column(conn, "tasks", "result_zip_path", "TEXT")
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_tasks_start_time ON tasks(start_time DESC)"
            )
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)"
            )

    def _ensure_column(self, conn, table_name, column_name, column_sql):
        existing_columns = {
            row["name"] for row in conn.execute(f"PRAGMA table_info({table_name})").fetchall()
        }
        if column_name in existing_columns:
            return
        conn.execute(f"ALTER TABLE {table_name} ADD COLUMN {column_name} {column_sql}")

    def create_task(self, task):
        payload = {
            "id": task["id"],
            "url": task["url"],
            "status": task["status"],
            "start_time": float(task["start_time"]),
            "error": task.get("error"),
            "progress": int(task.get("progress", 0)),
            "total_images": int(task.get("total_images", 0)),
            "image_concurrency": int(task.get("image_concurrency", 2)),
            "result_zip_path": task.get("result_zip_path"),
        }
        with self._lock, self._connect() as conn:
            conn.execute(
                """
                INSERT INTO tasks (
                    id, url, status, start_time, error, progress, total_images, image_concurrency, result_zip_path
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(id) DO UPDATE SET
                    url=excluded.url,
                    status=excluded.status,
                    start_time=excluded.start_time,
                    error=excluded.error,
                    progress=excluded.progress,
                    total_images=excluded.total_images,
                    image_concurrency=excluded.image_concurrency,
                    result_zip_path=excluded.result_zip_path
                """,
                (
                    payload["id"],
                    payload["url"],
                    payload["status"],
                    payload["start_time"],
                    payload["error"],
                    payload["progress"],
                    payload["total_images"],
                    payload["image_concurrency"],
                    payload["result_zip_path"],
                ),
            )

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

    def list_paginated(self, page, per_page):
        safe_page = max(1, int(page))
        safe_per_page = max(1, int(per_page))

        with self._lock, self._connect() as conn:
            total = conn.execute("SELECT COUNT(*) AS count FROM tasks").fetchone()["count"]
            total_pages = max(1, (total + safe_per_page - 1) // safe_per_page)
            safe_page = min(safe_page, total_pages)
            offset = (safe_page - 1) * safe_per_page
            rows = conn.execute(
                """
                SELECT
                    id,
                    url,
                    status,
                    start_time,
                    error,
                    progress,
                    total_images,
                    image_concurrency,
                    result_zip_path
                FROM tasks
                ORDER BY start_time DESC
                LIMIT ? OFFSET ?
                """,
                (safe_per_page, offset),
            ).fetchall()

        return [dict(row) for row in rows], total, total_pages, safe_page

    def get_task(self, task_id):
        with self._lock, self._connect() as conn:
            row = conn.execute(
                """
                SELECT
                    id,
                    url,
                    status,
                    start_time,
                    error,
                    progress,
                    total_images,
                    image_concurrency,
                    result_zip_path
                FROM tasks
                WHERE id = ?
                """,
                (task_id,),
            ).fetchone()
        if row is None:
            return None
        return dict(row)

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

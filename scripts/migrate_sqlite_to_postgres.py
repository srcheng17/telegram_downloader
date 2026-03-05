#!/usr/bin/env python3
"""Migrate task records from SQLite to PostgreSQL."""

import argparse
import os
import sqlite3
import sys
from pathlib import Path

ROOT_DIR = Path(__file__).resolve().parents[1]
if str(ROOT_DIR) not in sys.path:
    sys.path.insert(0, str(ROOT_DIR))

from telegram_downloader.repositories.task_store import TaskStore


TASK_COLUMNS = (
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
    "genres_raw",
    "genres_normalized",
)


def _read_sqlite_rows(sqlite_path):
    connection = sqlite3.connect(sqlite_path)
    connection.row_factory = sqlite3.Row
    try:
        rows = connection.execute("SELECT * FROM tasks ORDER BY start_time ASC").fetchall()
        return [dict(row) for row in rows]
    finally:
        connection.close()


def _normalize_row(raw):
    normalized = {}
    for key in TASK_COLUMNS:
        normalized[key] = raw.get(key)
    normalized["canonical_url"] = normalized["canonical_url"] or normalized["url"]
    normalized["progress"] = int(normalized["progress"] or 0)
    normalized["total_images"] = int(normalized["total_images"] or 0)
    normalized["image_concurrency"] = int(normalized["image_concurrency"] or 2)
    normalized["start_time"] = float(normalized["start_time"] or 0)
    return normalized


def _build_parser():
    parser = argparse.ArgumentParser(description="Migrate tasks from SQLite to PostgreSQL.")
    parser.add_argument(
        "--source-sqlite",
        default=os.environ.get("SQLITE_TASKS_DB_PATH", "data/tasks.db"),
        help="Path to source SQLite tasks DB. Default: data/tasks.db",
    )
    parser.add_argument(
        "--target-postgres",
        default=os.environ.get("TARGET_TASKS_DB_PATH", ""),
        help="PostgreSQL DSN, e.g. postgresql://user:pass@host:5432/dbname",
    )
    parser.add_argument(
        "--truncate-target",
        action="store_true",
        help="Clear target tasks table before import.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Only print migration stats, do not write to PostgreSQL.",
    )
    return parser


def main():
    parser = _build_parser()
    args = parser.parse_args()

    sqlite_path = Path(args.source_sqlite).expanduser()
    if not sqlite_path.is_file():
        raise SystemExit(f"Source SQLite DB not found: {sqlite_path}")

    if not args.target_postgres.strip():
        raise SystemExit(
            "Missing --target-postgres. You can also set TARGET_TASKS_DB_PATH env var."
        )

    if not args.target_postgres.startswith(("postgresql://", "postgres://")):
        raise SystemExit("Target DSN must be PostgreSQL (postgresql:// or postgres://).")

    rows = _read_sqlite_rows(str(sqlite_path))
    print(f"Found {len(rows)} rows in source SQLite DB: {sqlite_path}")
    if args.dry_run:
        print("Dry run enabled, no data written.")
        return 0

    target_store = TaskStore(args.target_postgres)
    if args.truncate_target:
        target_store.clear()
        print("Target tasks table cleared.")

    migrated = 0
    for raw in rows:
        target_store.create_task(_normalize_row(raw))
        migrated += 1

    print(f"Migration completed. Rows written: {migrated}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

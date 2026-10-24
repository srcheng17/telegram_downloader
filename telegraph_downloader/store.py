from __future__ import annotations

import json
import sqlite3
import uuid
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


class Store:
    def __init__(self, data_dir: Path):
        self.path = data_dir / "app.sqlite3"

    @contextmanager
    def connection(self):
        connection = sqlite3.connect(self.path, timeout=10)
        connection.row_factory = sqlite3.Row
        try:
            with connection:
                yield connection
        finally:
            connection.close()

    def initialize(self, default_dir: str = "") -> None:
        if self.path.is_symlink():
            raise RuntimeError("数据库文件不能是符号链接")
        with self.connection() as db:
            db.execute("PRAGMA journal_mode=WAL")
            db.execute("PRAGMA synchronous=FULL")
            version = db.execute("PRAGMA user_version").fetchone()[0]
            if version > 2:
                raise RuntimeError("数据库版本较新，请使用对应版本的应用")
            db.execute("BEGIN IMMEDIATE")
            db.execute("""
                CREATE TABLE IF NOT EXISTS jobs (
                    id TEXT PRIMARY KEY, kind TEXT NOT NULL, source TEXT NOT NULL,
                    metadata TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
                    phase TEXT NOT NULL DEFAULT '等待处理', error TEXT,
                    cancel_requested INTEGER NOT NULL DEFAULT 0,
                    artifact_path TEXT, filename TEXT, page_count INTEGER, size INTEGER, komga_path TEXT,
                    destination TEXT NOT NULL DEFAULT '{}',
                    created_at TEXT NOT NULL, updated_at TEXT NOT NULL
                )
            """)
            db.execute("CREATE INDEX IF NOT EXISTS jobs_status_created ON jobs(status, created_at)")
            db.execute("CREATE TABLE IF NOT EXISTS settings (name TEXT PRIMARY KEY, value TEXT NOT NULL)")
            if "destination" not in {row["name"] for row in db.execute("PRAGMA table_info(jobs)")}:
                db.execute("ALTER TABLE jobs ADD COLUMN destination TEXT NOT NULL DEFAULT '{}'")
            if version < 2:
                saved = {row["name"]: row["value"] for row in db.execute("SELECT name,value FROM settings")}
                if "libraries" not in saved and ("komga_dir" in saved or default_dir):
                    directory = saved.get("komga_dir", default_dir)
                    libraries = ([{"id": "default", "name": "默认书库", "path": str(Path(directory).resolve())}]
                                 if directory else [])
                    db.execute("INSERT INTO settings(name,value) VALUES('libraries',?)",
                               (json.dumps(libraries, ensure_ascii=False),))
                else:
                    libraries = json.loads(saved.get("libraries", "[]"))
                if len(libraries) == 1:
                    db.execute("UPDATE jobs SET destination=? WHERE destination='{}'",
                               (json.dumps(libraries[0], ensure_ascii=False),))
            db.execute("PRAGMA user_version=2")

    @staticmethod
    def decode(row) -> dict | None:
        if row is None:
            return None
        result = dict(row)
        result["metadata"] = json.loads(result["metadata"])
        result["destination"] = json.loads(result["destination"])
        result["cancel_requested"] = bool(result["cancel_requested"])
        return result

    def create(self, kind: str, source: str, metadata: dict, job_id: str | None = None,
               *, destination: dict | None = None) -> dict:
        job_id = job_id or uuid.uuid4().hex
        timestamp = now()
        with self.connection() as db:
            db.execute(
                "INSERT INTO jobs(id,kind,source,metadata,destination,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
                (job_id, kind, source, json.dumps(metadata, ensure_ascii=False),
                 json.dumps(destination or {}, ensure_ascii=False), timestamp, timestamp),
            )
        return self.get(job_id)

    def get(self, job_id: str) -> dict | None:
        with self.connection() as db:
            return self.decode(db.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone())

    def list(self, limit: int = 100) -> list[dict]:
        with self.connection() as db:
            return [self.decode(row) for row in db.execute(
                "SELECT * FROM jobs ORDER BY created_at DESC, rowid DESC LIMIT ?", (limit,)
            )]

    def recover(self) -> None:
        with self.connection() as db:
            db.execute(
                "UPDATE jobs SET status='failed', phase='处理已中断', "
                "error='服务重启中断，请重试', cancel_requested=0, updated_at=? "
                "WHERE status IN ('running','delivering')",
                (now(),),
            )

    def claim(self) -> dict | None:
        with self.connection() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                "SELECT id FROM jobs WHERE status='queued' ORDER BY created_at, rowid LIMIT 1"
            ).fetchone()
            if not row:
                return None
            db.execute(
                "UPDATE jobs SET status='running',phase='准备处理',updated_at=? WHERE id=?",
                (now(), row["id"]),
            )
            return self.decode(db.execute("SELECT * FROM jobs WHERE id=?", (row["id"],)).fetchone())

    def progress(self, job_id: str, phase: str) -> None:
        with self.connection() as db:
            db.execute(
                "UPDATE jobs SET phase=?,updated_at=? WHERE id=? AND status='running' AND cancel_requested=0",
                (phase, now(), job_id),
            )

    def begin_delivery(self, job_id: str, artifact) -> bool:
        with self.connection() as db:
            return db.execute(
                "UPDATE jobs SET status='delivering', phase='正在放入 Komga', artifact_path=?, "
                "filename=?,page_count=?,size=?,updated_at=? "
                "WHERE id=? AND status='running' AND cancel_requested=0",
                (str(artifact.path), artifact.filename, artifact.page_count, artifact.size, now(), job_id),
            ).rowcount == 1

    def complete(self, job_id: str, komga_path: Path) -> bool:
        with self.connection() as db:
            return db.execute(
                "UPDATE jobs SET status='succeeded', phase='已放入 Komga 书库', error=NULL, "
                "artifact_path=NULL,komga_path=?,updated_at=? WHERE id=? AND status='delivering'",
                (str(komga_path), now(), job_id),
            ).rowcount == 1

    def finish(self, job_id: str, status: str, error: str | None = None) -> None:
        if status not in {"failed", "cancelled"}:
            raise ValueError("无效的结束状态")
        with self.connection() as db:
            db.execute(
                "UPDATE jobs SET status=?,phase=?,error=?,updated_at=? "
                "WHERE id=? AND status IN ('running','delivering')",
                (status, "已取消" if status == "cancelled" else "处理失败", error, now(), job_id),
            )

    def cancel(self, job_id: str) -> dict:
        with self.connection() as db:
            changed = db.execute(
                "UPDATE jobs SET cancel_requested=1, status=CASE WHEN status='queued' THEN 'cancelled' "
                "ELSE status END,phase=CASE WHEN status='queued' THEN '已取消' ELSE '正在取消' END,"
                "updated_at=? WHERE id=? AND status IN ('queued','running')",
                (now(), job_id),
            ).rowcount
        if not changed:
            if not self.get(job_id):
                raise KeyError(job_id)
            raise ValueError("当前任务不能取消")
        return self.get(job_id)

    def retry(self, job_id: str) -> dict:
        with self.connection() as db:
            changed = db.execute(
                "UPDATE jobs SET status='queued',phase='等待处理',error=NULL,cancel_requested=0,"
                "updated_at=? "
                "WHERE id=? AND status IN ('failed','cancelled')",
                (now(), job_id),
            ).rowcount
        if not changed:
            raise ValueError("当前任务不能重试")
        return self.get(job_id)

    def settings(self, default_dir: str = "") -> dict:
        with self.connection() as db:
            saved = {row["name"]: row["value"] for row in db.execute(
                "SELECT name,value FROM settings WHERE name IN ('libraries','komga_dir','komga_url')"
            )}
        if "libraries" in saved:
            libraries = json.loads(saved["libraries"])
        else:
            directory = saved.get("komga_dir", default_dir)
            libraries = ([{"id": "default", "name": "默认书库", "path": str(Path(directory).resolve())}]
                         if directory else [])
        return {"libraries": libraries, "komga_dir": libraries[0]["path"] if libraries else "",
                "komga_url": saved.get("komga_url", "")}

    def save_settings(self, libraries: list[dict], komga_url: str) -> dict:
        with self.connection() as db:
            db.executemany(
                "INSERT INTO settings(name,value) VALUES(?,?) "
                "ON CONFLICT(name) DO UPDATE SET value=excluded.value",
                (("libraries", json.dumps(libraries, ensure_ascii=False)),
                 ("komga_dir", libraries[0]["path"] if libraries else ""), ("komga_url", komga_url)),
            )
        return self.settings()

import json
import sqlite3
from types import SimpleNamespace

import pytest

from telegraph_downloader.store import Store


def test_restart_preserves_queue_and_marks_interrupted_task(tmp_path):
    store = Store(tmp_path)
    store.initialize()
    first = store.create("url", "https://telegra.ph/first", {})
    second = store.create("url", "https://telegra.ph/second", {})
    assert store.claim()["id"] == first["id"]
    reopened = Store(tmp_path)
    reopened.recover()
    assert reopened.get(first["id"])["status"] == "failed"
    assert "重启中断" in reopened.get(first["id"])["error"]
    assert reopened.get(second["id"])["status"] == "queued"
    assert reopened.claim()["id"] == second["id"]


def test_cancel_wins_before_success_commit(tmp_path):
    store = Store(tmp_path)
    store.initialize()
    job = store.create("url", "https://telegra.ph/book", {})
    store.claim()
    store.cancel(job["id"])
    artifact = SimpleNamespace(path=tmp_path / "book.cbz", filename="book.cbz", page_count=2, size=100)
    assert not store.begin_delivery(job["id"], artifact)
    store.finish(job["id"], "cancelled")
    assert store.get(job["id"])["status"] == "cancelled"
    retried = store.retry(job["id"])
    assert retried["status"] == "queued"
    assert not retried["cancel_requested"]


def test_settings_and_metadata_roundtrip(tmp_path):
    store = Store(tmp_path)
    store.initialize()
    metadata = {"comic_name": "书页<&>", "summary": "第一行\n第二行"}
    job = store.create("url", "https://telegra.ph/book", metadata)
    assert Store(tmp_path).get(job["id"])["metadata"] == metadata
    assert store.settings() == {"libraries": [], "komga_dir": "", "komga_url": ""}
    libraries = [{"id": "books", "name": "漫画", "path": "/books"}]
    store.save_settings(libraries, "http://komga:25600")
    assert Store(tmp_path).settings() == {
        "libraries": libraries, "komga_dir": "/books", "komga_url": "http://komga:25600",
    }


def test_interrupted_delivery_preserves_cbz_for_idempotent_retry(tmp_path):
    store = Store(tmp_path)
    store.initialize()
    job = store.create("url", "https://telegra.ph/book", {})
    store.claim()
    artifact = SimpleNamespace(path=tmp_path / "book.cbz", filename="book.cbz", page_count=2, size=100)
    assert store.begin_delivery(job["id"], artifact)
    store.recover()
    recovered = store.get(job["id"])
    assert recovered["status"] == "failed"
    assert recovered["artifact_path"] == str(artifact.path)
    assert store.retry(job["id"])["filename"] == "book.cbz"


def create_version_one_database(tmp_path, directory=None):
    cached = tmp_path / "preserved.cbz"
    cached.write_bytes(b"original cached comic")
    with sqlite3.connect(tmp_path / "app.sqlite3") as db:
        db.executescript("""
            CREATE TABLE jobs (
                id TEXT PRIMARY KEY, kind TEXT NOT NULL, source TEXT NOT NULL,
                metadata TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
                phase TEXT NOT NULL DEFAULT '等待处理', error TEXT,
                cancel_requested INTEGER NOT NULL DEFAULT 0,
                artifact_path TEXT, filename TEXT, page_count INTEGER, size INTEGER, komga_path TEXT,
                created_at TEXT NOT NULL, updated_at TEXT NOT NULL
            );
            CREATE TABLE settings (name TEXT PRIMARY KEY, value TEXT NOT NULL);
            PRAGMA user_version=1;
        """)
        for job_id, status in (("waiting", "queued"), ("cached", "failed")):
            db.execute(
                "INSERT INTO jobs(id,kind,source,metadata,status,artifact_path,filename,page_count,size,"
                "created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
                (job_id, "url", "https://telegra.ph/old", json.dumps({"comic_name": "旧漫画"}), status,
                 str(cached) if status == "failed" else None, "old.cbz", 2, 21, "2026-10-24", "2026-10-24"),
            )
        if directory is not None:
            db.execute("INSERT INTO settings(name,value) VALUES('komga_dir',?)", (str(directory),))
        db.execute("INSERT INTO settings(name,value) VALUES('komga_url','https://komga.example')")
    return cached


@pytest.mark.parametrize("saved_directory", [True, False])
def test_version_one_migration_freezes_legacy_target_and_preserves_cache(tmp_path, saved_directory):
    saved, environment = tmp_path / "saved", tmp_path / "environment"
    saved.mkdir()
    environment.mkdir()
    cached = create_version_one_database(tmp_path, saved if saved_directory else None)
    store = Store(tmp_path)
    store.initialize(str(environment))
    destination = {"id": "default", "name": "默认书库",
                   "path": str((saved if saved_directory else environment).resolve())}
    assert store.settings()["libraries"] == [destination]
    assert store.settings()["komga_url"] == "https://komga.example"
    assert store.get("waiting")["status"] == "queued"
    assert store.get("cached")["status"] == "failed"
    assert store.get("cached")["artifact_path"] == str(cached)
    assert store.get("cached")["metadata"] == {"comic_name": "旧漫画"}
    assert cached.read_bytes() == b"original cached comic"
    for job_id in ("waiting", "cached"):
        assert store.get(job_id)["destination"] == destination
    with store.connection() as db:
        assert db.execute("PRAGMA user_version").fetchone()[0] == 2
    store.save_settings([{"id": "new", "name": "新书库", "path": str(environment)}], "")
    Store(tmp_path).initialize(str(tmp_path / "another-default"))
    assert store.get("waiting")["destination"] == destination
    assert store.retry("cached")["destination"] == destination
    assert cached.read_bytes() == b"original cached comic"


def test_unknown_legacy_target_never_rebinds_after_settings_or_restart(tmp_path):
    create_version_one_database(tmp_path)
    store = Store(tmp_path)
    store.initialize()
    assert store.get("waiting")["destination"] == {}
    store.save_settings([{"id": "new", "name": "新书库", "path": str(tmp_path / "books")}], "")
    store.initialize(str(tmp_path / "environment"))
    assert store.get("waiting")["destination"] == {}
    assert store.retry("cached")["destination"] == {}


def test_saved_empty_library_list_does_not_restore_environment_default(tmp_path):
    store = Store(tmp_path)
    store.initialize(str(tmp_path / "old"))
    store.save_settings([], "")
    store.initialize(str(tmp_path / "new"))
    assert store.settings(str(tmp_path / "new"))["libraries"] == []


def test_newer_schema_is_not_downgraded(tmp_path):
    store = Store(tmp_path)
    store.initialize()
    with store.connection() as db:
        db.execute("PRAGMA user_version=3")
    with pytest.raises(RuntimeError, match="数据库版本较新"):
        store.initialize()

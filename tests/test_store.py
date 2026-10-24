from types import SimpleNamespace

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
    assert store.settings() == {"komga_dir": "", "komga_url": ""}
    store.save_settings("/books", "http://komga:25600")
    assert Store(tmp_path).settings() == {"komga_dir": "/books", "komga_url": "http://komga:25600"}


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

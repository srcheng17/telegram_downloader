import base64
import io
import threading
import time
import zipfile
from dataclasses import replace
from pathlib import Path
from xml.etree import ElementTree

import pytest
from fastapi.testclient import TestClient

from telegraph_downloader.config import Config
from telegraph_downloader.main import create_app
from telegraph_downloader.media import MediaCancelled, MediaError

PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jw1cAAAAASUVORK5CYII="
)


def archive_bytes():
    target = io.BytesIO()
    with zipfile.ZipFile(target, "w") as archive:
        archive.writestr("b/02.png", PNG + b"second")
        archive.writestr("a/01.png", PNG)
    return target.getvalue()


def wait_job(client, job_id, states=("succeeded", "failed", "cancelled")):
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        job = client.get(f"/api/jobs/{job_id}").json()
        if job["status"] in states:
            return job
        time.sleep(0.02)
    pytest.fail(f"task did not reach {states}: {job}")


@pytest.fixture
def config(tmp_path):
    destination = tmp_path / "komga"
    destination.mkdir()
    return Config(data_dir=tmp_path / "data", komga_dir=destination)


def test_real_upload_automatically_delivers_metadata_to_komga(config):
    with TestClient(create_app(config)) as client:
        response = client.post("/api/jobs/upload", files={"file": ("漫画.zip", archive_bytes())}, data={
            "author": "作者甲", "series_name": "系列乙", "series_number": "2",
            "comic_name": "标题<&>", "summary": "简介\n下一行", "tags": "标签甲,标签乙", "genres": "冒险",
        })
        assert response.status_code == 202, response.text
        job = wait_job(client, response.json()["id"])
        assert job["status"] == "succeeded", job
        assert job["status_label"] == "已入库"
        assert job["page_count"] == 2
        assert job["available_actions"] == []
        targets = list(config.komga_dir.glob("系列乙/*.cbz"))
        assert len(targets) == 1
        with zipfile.ZipFile(targets[0]) as archive:
            images = sorted(name for name in archive.namelist() if name.endswith(".png"))
            assert [archive.read(name) for name in images] == [PNG, PNG + b"second"]
            xml = ElementTree.fromstring(archive.read("ComicInfo.xml"))
            assert xml.findtext("Writer") == "作者甲"
            assert xml.findtext("Title") == "标题<&>"
            assert xml.findtext("Number") == "2"
            assert xml.findtext("Summary") == "简介\n下一行"
            assert xml.findtext("Genre") == "冒险"
            assert "标签甲" in xml.findtext("Tags")
        assert client.get(f"/api/jobs/{job['id']}/download").status_code == 404
        assert client.post(f"/api/jobs/{job['id']}/komga").status_code == 404
        assert not (config.data_dir / "inputs" / job["id"]).exists()
        assert not list((config.data_dir / "artifacts").iterdir())


def test_failed_delivery_retries_cached_cbz_without_source(config, monkeypatch):
    import telegraph_downloader.jobs as jobs

    original = jobs.copy_to_komga

    def unavailable(*args):
        raise MediaError("书库暂时不可写")

    monkeypatch.setattr(jobs, "copy_to_komga", unavailable)
    app = create_app(config)
    with TestClient(app) as client:
        response = client.post("/api/jobs/upload", files={"file": ("book.zip", archive_bytes())})
        job = wait_job(client, response.json()["id"])
        assert job["status"] == "failed"
        internal = app.state.store.get(job["id"])
        assert Path(internal["artifact_path"]).is_file()
        Path(internal["source"]).unlink()
        assert "retry" in client.get(f"/api/jobs/{job['id']}").json()["available_actions"]
        monkeypatch.setattr(jobs, "copy_to_komga", original)
        assert client.post(f"/api/jobs/{job['id']}/retry").status_code == 200
        assert wait_job(client, job["id"])["status"] == "succeeded"
        assert len(list(config.komga_dir.glob("tankobon/*.cbz"))) == 1
        assert not list((config.data_dir / "artifacts").iterdir())


def test_delivery_commit_phase_cannot_be_cancelled(config, monkeypatch):
    import telegraph_downloader.jobs as jobs

    entered, release = threading.Event(), threading.Event()
    original = jobs.copy_to_komga

    def delivering(*args):
        entered.set()
        assert release.wait(5)
        return original(*args)

    monkeypatch.setattr(jobs, "copy_to_komga", delivering)
    with TestClient(create_app(config)) as client:
        response = client.post("/api/jobs/upload", files={"file": ("book.zip", archive_bytes())})
        try:
            assert entered.wait(3)
            job = client.get(f"/api/jobs/{response.json()['id']}").json()
            assert job["status"] == "delivering"
            assert job["available_actions"] == []
            assert client.post(f"/api/jobs/{job['id']}/cancel").status_code == 409
        finally:
            release.set()
        assert wait_job(client, job["id"])["status"] == "succeeded"


def test_input_validation_and_body_limit(config):
    with TestClient(create_app(replace(config, max_upload_bytes=32))) as client:
        for url in ["http://127.0.0.1/a", "https://telegra.ph.evil.example/a", "file:///etc/passwd",
                    "https://user:password@telegra.ph/a", "https://telegra.ph/"]:
            assert client.post("/api/jobs/url", json={"url": url}).status_code == 400
        assert client.post("/api/jobs/upload", files={"file": ("file.exe", b"x")}).status_code == 400
        assert client.post("/api/jobs/upload", files={"file": ("file.zip", b"x" * 33)}).status_code == 413
        assert client.post("/api/jobs/url", json={"url": "https://telegra.ph/a", "author": "a" * 201}).status_code == 422
        assert client.post("/api/jobs/url", content=b"x" * 140000).status_code == 413
        assert client.get("/api/jobs").json()["jobs"] == []
        assert list((config.data_dir / "inputs").iterdir()) == []


def test_streamed_body_limit_without_content_length(config):
    with TestClient(create_app(config)) as client:
        response = client.post("/api/jobs/url", content=(b"x" * 10000 for _ in range(15)),
                               headers={"content-type": "application/json"})
        assert response.status_code == 413


def test_failed_upload_can_retry_with_retained_input(config):
    with TestClient(create_app(config)) as client:
        response = client.post("/api/jobs/upload", files={"file": ("broken.zip", b"invalid zip")})
        job = wait_job(client, response.json()["id"])
        assert job["status"] == "failed"
        assert "retry" in job["available_actions"]
        assert (config.data_dir / "inputs" / job["id"] / "source.zip").exists()
        assert client.post(f"/api/jobs/{job['id']}/retry").status_code == 200
        assert wait_job(client, job["id"])["status"] == "failed"
        assert not list((config.data_dir / "artifacts").iterdir())


def test_running_cancel_stops_executor_and_queued_cancel_does_not_execute(config):
    started = threading.Event()
    calls = []

    def blocking(job, config, cancel, progress):
        calls.append(job["id"])
        started.set()
        assert cancel.wait(5)
        raise MediaCancelled()

    with TestClient(create_app(config, executor=blocking)) as client:
        first = client.post("/api/jobs/url", json={"url": "https://telegra.ph/a"}).json()
        assert started.wait(3)
        second = client.post("/api/jobs/url", json={"url": "https://telegra.ph/b"}).json()
        assert client.post(f"/api/jobs/{second['id']}/cancel").json()["status"] == "cancelled"
        assert client.post(f"/api/jobs/{first['id']}/cancel").status_code == 200
        assert wait_job(client, first["id"])["status"] == "cancelled"
        assert calls == [first["id"]]


def test_only_one_instance_per_data_directory(config):
    with TestClient(create_app(config)):
        with pytest.raises(RuntimeError, match="已有运行实例"):
            with TestClient(create_app(config)):
                pass


def test_cross_origin_mutation_rejected_and_settings_persist(config):
    value = {"komga_dir": str(config.komga_dir), "komga_url": "http://komga.example:25600/"}
    with TestClient(create_app(config)) as client:
        for origin in ["https://evil.example", "https://[broken"]:
            assert client.post("/api/settings", json=value, headers={"origin": origin}).status_code == 403
        response = client.post("/api/settings", json=value)
        assert response.status_code == 200, response.text
        assert response.json()["komga_url"] == "http://komga.example:25600"
    with TestClient(create_app(replace(config, komga_dir=None))) as client:
        value = client.get("/api/settings").json()
        assert value["configured"]
        assert value["komga_dir"] == str(config.komga_dir)
        assert value["komga_url"] == "http://komga.example:25600"


@pytest.mark.parametrize("invalid", [[], {}, None, 1])
def test_settings_reject_invalid_field_types(config, invalid):
    with TestClient(create_app(config)) as client:
        assert client.post("/api/settings", json={"komga_dir": invalid}).status_code == 422


def test_settings_validate_url_and_existing_writable_directory(config):
    with TestClient(create_app(config)) as client:
        for path in ["relative", "/books/\x00invalid"]:
            assert client.post("/api/settings", json={"komga_dir": path}).status_code == 400
        for url in ["javascript:alert(1)", "https://user:secret@komga.example", "https://[broken"]:
            assert client.post("/api/settings", json={"komga_dir": str(config.komga_dir),
                                                      "komga_url": url}).status_code == 400


def test_unconfigured_komga_rejects_submission_before_upload(config):
    with TestClient(create_app(replace(config, komga_dir=None))) as client:
        assert not client.get("/api/settings").json()["configured"]
        assert client.post("/api/jobs/upload", files={"file": ("book.zip", archive_bytes())}).status_code == 409
        assert client.post("/api/jobs/url", json={"url": "https://telegra.ph/book"}).status_code == 409
        assert client.get("/api/jobs").json()["jobs"] == []


def test_home_contains_no_job_history_or_download_controls(config):
    with TestClient(create_app(config)) as client:
        home = client.get("/").text
        assert 'data-testid="archive-form"' in home
        assert 'data-testid="job-list"' not in home
        assert 'data-testid="download-job"' not in home
        assert 'data-testid="job-list"' in client.get("/tasks").text

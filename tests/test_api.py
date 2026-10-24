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
from telegraph_downloader.media import MediaCancelled, MediaError, execute
from telegraph_downloader.store import Store

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


@pytest.fixture
def libraries(config):
    result = []
    for library_id, name in (("japanese", "日漫"), ("chinese", "国漫")):
        directory = config.komga_dir / name
        directory.mkdir()
        result.append({"id": library_id, "name": name, "path": str(directory.resolve())})
    return result


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
        changed = config.komga_dir.parent / "changed-library"
        changed.mkdir()
        response = client.post("/api/settings", json={"libraries": [
            {"id": "changed", "name": "修改后的书库", "path": str(changed)},
        ]})
        assert response.status_code == 200
        monkeypatch.setattr(jobs, "copy_to_komga", original)
        assert client.post(f"/api/jobs/{job['id']}/retry").status_code == 200
        assert wait_job(client, job["id"])["status"] == "succeeded"
        assert len(list(config.komga_dir.glob("tankobon/*.cbz"))) == 1
        assert not list(changed.iterdir())
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


def test_real_upload_chooses_only_selected_library_and_keeps_metadata(config, libraries):
    with TestClient(create_app(config)) as client:
        assert client.post("/api/settings", json={"libraries": libraries}).status_code == 200
        response = client.post("/api/jobs/upload", files={"file": ("漫画.zip", archive_bytes())}, data={
            "library_id": "chinese", "author": "作者", "series_name": "系列", "series_number": "3",
            "comic_name": "国漫目标", "summary": "简介", "tags": "标签", "genres": "类型",
        })
        assert response.status_code == 202, response.text
        job = wait_job(client, response.json()["id"])
        assert job["status"] == "succeeded", job
        assert job["library_id"] == "chinese" and job["library_name"] == "国漫"
        assert "library_id" not in job["metadata"]
        assert "destination" not in job and "path" not in job
        assert not list(Path(libraries[0]["path"]).rglob("*.cbz"))
        targets = list(Path(libraries[1]["path"]).glob("系列/*.cbz"))
        assert len(targets) == 1
        with zipfile.ZipFile(targets[0]) as archive:
            images = sorted(name for name in archive.namelist() if name.endswith(".png"))
            assert [archive.read(name) for name in images] == [PNG, PNG + b"second"]
            xml = ElementTree.fromstring(archive.read("ComicInfo.xml"))
            assert xml.findtext("Title") == "国漫目标"
            assert xml.findtext("Number") == "3"
            assert xml.find("library_id") is None


def test_url_submission_passes_frozen_library_without_polluting_metadata(config, libraries):
    executed = []

    def offline_executor(job, *_):
        executed.append(job)
        raise MediaError("离线边界验证")

    with TestClient(create_app(config, executor=offline_executor)) as client:
        assert client.post("/api/settings", json={"libraries": libraries}).status_code == 200
        response = client.post("/api/jobs/url", json={
            "url": "https://telegra.ph/library-test", "library_id": "japanese", "comic_name": "日漫任务",
        })
        assert response.status_code == 202, response.text
        job = wait_job(client, response.json()["id"])
        assert job["status"] == "failed"
        assert job["library_name"] == "日漫"
        assert executed[0]["destination"] == libraries[0]
        assert executed[0]["metadata"]["comic_name"] == "日漫任务"
        assert "library_id" not in executed[0]["metadata"]


def test_legacy_job_without_destination_requires_resubmission_even_when_configured(config):
    config.prepare()
    store = Store(config.data_dir)
    store.initialize()
    old = store.create("url", "https://telegra.ph/old", {})
    store.claim()
    store.finish(old["id"], "failed", "旧任务没有可确定的目标")
    with TestClient(create_app(config)) as client:
        assert client.get("/api/settings").json()["configured"]
        assert client.get(f"/api/jobs/{old['id']}").json()["available_actions"] == []
        response = client.post(f"/api/jobs/{old['id']}/retry")
        assert response.status_code == 409
        assert "重新提交" in response.json()["detail"]
        assert store.get(old["id"])["destination"] == {}


def test_queued_jobs_keep_original_library_after_rename_move_and_delete(config, libraries):
    entered, release = threading.Event(), threading.Event()

    def hold_first(job, *args):
        if job["metadata"]["comic_name"] == "等待中的日漫":
            entered.set()
            assert release.wait(5)
        return execute(job, *args)

    with TestClient(create_app(config, executor=hold_first)) as client:
        assert client.post("/api/settings", json={"libraries": libraries}).status_code == 200
        try:
            first = client.post("/api/jobs/upload", files={"file": ("first.zip", archive_bytes())},
                                data={"library_id": "japanese", "comic_name": "等待中的日漫"}).json()
            assert entered.wait(3)
            second = client.post("/api/jobs/upload", files={"file": ("second.zip", archive_bytes())},
                                 data={"library_id": "chinese", "comic_name": "排队中的国漫"}).json()
            assert second["status"] == "queued"
            changed = config.komga_dir / "changed"
            changed.mkdir()
            response = client.post("/api/settings", json={"libraries": [
                {"id": "chinese", "name": "新名称", "path": str(changed)},
            ]})
            assert response.status_code == 200, response.text
        finally:
            release.set()
        assert wait_job(client, first["id"])["status"] == "succeeded"
        second = wait_job(client, second["id"])
        assert second["status"] == "succeeded"
        assert second["library_name"] == "国漫"
        assert len(list(Path(libraries[0]["path"]).rglob("*.cbz"))) == 1
        assert len(list(Path(libraries[1]["path"]).rglob("*.cbz"))) == 1
        assert not list(changed.iterdir())


def test_generation_rejects_library_root_replaced_with_another_library_symlink(config, libraries):
    entered, release = threading.Event(), threading.Event()

    def held_generation(job, *args):
        entered.set()
        assert release.wait(5)
        return execute(job, *args)

    app = create_app(config, executor=held_generation)
    with TestClient(app) as client:
        assert client.post("/api/settings", json={"libraries": libraries}).status_code == 200
        try:
            response = client.post("/api/jobs/upload", files={"file": ("book.zip", archive_bytes())},
                                   data={"library_id": "japanese", "comic_name": "不能跨库的日漫"})
            assert response.status_code == 202, response.text
            assert entered.wait(3)
            original = Path(libraries[0]["path"])
            moved = original.with_name("original-japanese")
            original.rename(moved)
            original.symlink_to(libraries[1]["path"], target_is_directory=True)
        finally:
            release.set()
        job = wait_job(client, response.json()["id"])
        assert job["status"] == "failed", job
        assert job["library_name"] == "日漫"
        assert Path(app.state.store.get(job["id"])["artifact_path"]).is_file()
        assert not list(Path(libraries[1]["path"]).rglob("*.cbz"))
        assert not list(moved.rglob("*.cbz"))


def test_submission_rejects_saved_root_with_replaced_symlink_ancestor(config, libraries):
    def unexpected_execution(*_):
        raise AssertionError("Rejected submissions must not execute")

    with TestClient(create_app(config, executor=unexpected_execution)) as client:
        assert client.post("/api/settings", json={"libraries": libraries[:1]}).status_code == 200
        original_parent = config.komga_dir
        moved = original_parent.with_name("original-root")
        original_parent.rename(moved)
        redirected = original_parent.with_name("redirected-root")
        (redirected / "日漫").mkdir(parents=True)
        original_parent.symlink_to(redirected, target_is_directory=True)
        response = client.post("/api/jobs/url", json={"url": "https://telegra.ph/book", "library_id": "japanese"})
        assert response.status_code == 409, response.text
        response = client.post("/api/jobs/upload", files={"file": ("book.zip", archive_bytes())},
                               data={"library_id": "japanese"})
        assert response.status_code == 409, response.text
        assert client.get("/api/jobs").json()["jobs"] == []
        assert not list((config.data_dir / "inputs").iterdir())
        assert not list(redirected.rglob("*.cbz"))


def test_invalid_or_missing_library_selection_has_no_queued_job_or_upload(config, libraries):
    with TestClient(create_app(config)) as client:
        assert client.post("/api/settings", json={"libraries": libraries}).status_code == 200
        for selection in ({}, {"library_id": "unknown"}):
            assert client.post("/api/jobs/url", json={"url": "https://telegra.ph/book", **selection}).status_code == 400
            assert client.post("/api/jobs/upload", files={"file": ("book.zip", archive_bytes())},
                               data=selection).status_code == 400
        assert client.post("/api/settings", json={"libraries": libraries[:1]}).status_code == 200
        assert client.post("/api/jobs/upload", files={"file": ("book.zip", archive_bytes())},
                           data={"library_id": "chinese"}).status_code == 400
        assert client.get("/api/jobs").json()["jobs"] == []
        assert not list((config.data_dir / "inputs").iterdir())


def test_library_ids_are_generated_persisted_and_legacy_alias_remains(config, libraries):
    with TestClient(create_app(config)) as client:
        response = client.post("/api/settings", json={"libraries": [{
            "name": " 日漫 ", "path": libraries[0]["path"],
        }]})
        assert response.status_code == 200, response.text
        saved = response.json()
        assert saved["libraries"][0]["id"]
        assert saved["libraries"][0]["name"] == "日漫"
        assert saved["komga_dir"] == libraries[0]["path"]
        assert client.post("/api/settings", json={"libraries": saved["libraries"]}).json() == saved
        assert client.post("/api/settings", json={"komga_dir": libraries[1]["path"]}).json()["libraries"] == [
            {"id": "default", "name": "默认书库", "path": libraries[1]["path"]},
        ]


@pytest.mark.parametrize("invalid", ["id", "name", "root", "child", "symlink", "empty-name", "control"])
def test_library_configuration_rejects_duplicates_and_overlaps_without_saving(config, libraries, invalid):
    entries = [dict(entry) for entry in libraries]
    if invalid == "id":
        entries[1]["id"] = " japanese "
    elif invalid == "name":
        entries[1]["name"] = " 日漫 "
    elif invalid == "root":
        entries[1]["path"] = entries[0]["path"]
    elif invalid == "child":
        child = Path(entries[0]["path"]) / "nested"
        child.mkdir()
        entries[1]["path"] = str(child)
    elif invalid == "symlink":
        alias = config.komga_dir / "alias"
        alias.symlink_to(entries[0]["path"], target_is_directory=True)
        entries[1]["path"] = str(alias)
    elif invalid == "control":
        entries[1]["id"] = "bad\x00id"
    else:
        entries[1]["name"] = "   "
    with TestClient(create_app(config)) as client:
        original = client.get("/api/settings").json()
        response = client.post("/api/settings", json={"libraries": entries})
        assert response.status_code == 400, response.text
        assert client.get("/api/settings").json() == original

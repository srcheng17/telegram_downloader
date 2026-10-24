from __future__ import annotations

import base64
import io
import json
import os
import shutil
import socketserver
import stat
import subprocess
import sys
import tarfile
import threading
import time
import zipfile
from dataclasses import replace
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from xml.etree import ElementTree

import pytest

from telegraph_downloader.config import Config
from telegraph_downloader.media import (
    MediaCancelled,
    MediaError,
    canonical_url,
    copy_to_komga,
    execute,
    validate_artifact,
)

FIXTURES = Path(__file__).parent / "fixtures"
PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jh1cAAAAASUVORK5CYII="
)


@pytest.fixture
def config(tmp_path):
    value = Config(data_dir=tmp_path / "data", komga_dir=tmp_path / "komga")
    value.prepare()
    return value


def upload_job(config, members, *, task_id="upload", suffix=".zip", metadata=None):
    directory = config.data_dir / "inputs" / task_id
    directory.mkdir(parents=True)
    source = directory / ("source" + suffix)
    if suffix == ".zip":
        with zipfile.ZipFile(source, "w") as archive:
            for name, payload in members:
                archive.writestr(name, payload)
    return {
        "id": task_id,
        "kind": "upload",
        "source": str(source),
        "metadata": metadata or {},
    }


def url_job(scenario="ok", task_id="download"):
    return {"id": task_id, "kind": "url", "source": "https://telegra.ph/" + scenario, "metadata": {}}


def run(job, config, cancel=None):
    return execute(job, config, cancel or threading.Event(), lambda phase: None)


def fake_config(config):
    return replace(config, gallery_command=(sys.executable, str(FIXTURES / "fake_gallery.py")))


def test_zip_preserves_order_bytes_metadata_and_atomic_komga(config, monkeypatch):
    metadata = {
        "author": "作者 & A",
        "series_name": "系列",
        "series_number": "3.5",
        "comic_name": "漫画 <一>",
        "summary": "第一行\n第二行\x00",
        "tags": "a,b",
        "genres": "漫画,艺术",
    }
    job = upload_job(config, [("z/02.jpg", b"second"), ("a/01.png", PNG), ("ComicInfo.xml", b"old")], metadata=metadata)
    monkeypatch.setattr("telegraph_downloader.media.time.time", lambda: 1700000000)
    phases = []
    artifact = execute(job, config, threading.Event(), phases.append)
    assert artifact.path == config.data_dir / "artifacts" / "upload.cbz"
    assert artifact.filename == "作者 & A_系列_漫画 一_1700000000.cbz"
    assert artifact.page_count == 2 and artifact.size == artifact.path.stat().st_size
    with zipfile.ZipFile(artifact.path) as archive:
        assert archive.namelist() == ["ComicInfo.xml", "00001.png", "00002.jpg"]
        assert archive.read("00001.png") == PNG
        assert archive.read("00002.jpg") == b"second"
        assert archive.testzip() is None
        comic = ElementTree.fromstring(archive.read("ComicInfo.xml"))
        assert {element.tag: element.text for element in comic} == {
            "Writer": "作者 & A", "Series": "系列", "Number": "3.5", "Title": "漫画 <一>",
            "Summary": "第一行\n第二行", "Tags": "a,b", "Genre": "漫画,艺术",
        }
    assert phases == ["读取压缩包", "生成 CBZ"]
    assert Path(job["source"]).exists()
    assert not (config.data_dir / "work" / job["id"]).exists()
    target = copy_to_komga(config, artifact.path, artifact.filename, metadata["series_name"])
    assert target == config.komga_dir / "系列" / artifact.filename
    assert target.read_bytes() == artifact.path.read_bytes()
    assert copy_to_komga(config, artifact.path, artifact.filename, "系列") == target
    assert not list(target.parent.glob("*.tmp"))
    assert stat.S_IMODE(target.stat().st_mode) == 0o644


@pytest.mark.parametrize("scenario", ["partial", "missing", "empty", "bool", "many", "part", "unexpected"])
def test_gallery_rejects_incomplete_results_without_publishing(config, scenario):
    with pytest.raises(MediaError):
        run(url_job(scenario), fake_config(config))
    assert not (config.data_dir / "artifacts" / "download.cbz").exists()
    assert not (config.data_dir / "work" / "download").exists()


def test_manifest_download_success(config):
    artifact = run(url_job(), fake_config(config))
    with zipfile.ZipFile(artifact.path) as archive:
        assert archive.read("00001.png") == b"page one"
        assert archive.read("00002.png") == b"page two"
    assert artifact.filename.startswith("未知作者_未命名漫画_")


def test_download_and_upload_size_checks(config):
    with pytest.raises(MediaError):
        run(url_job(), replace(fake_config(config), max_image_bytes=3))
    job = upload_job(config, [("one.png", PNG)])
    with pytest.raises(MediaError, match="上传源文件"):
        run(job, replace(config, max_upload_bytes=1))


@pytest.mark.parametrize(
    ("members", "limits"),
    [
        ([("one.png", b"12345")], {"max_image_bytes": 4}),
        ([("one.png", b"12"), ("two.png", b"34")], {"max_images": 1}),
        ([("one.png", b"1"), ("hidden.txt", b"12345")], {"max_total_bytes": 5}),
        ([("only.txt", b"no pictures")], {}),
        ([("empty.png", b"")], {}),
    ],
)
def test_archive_budgets_and_empty_results(config, members, limits):
    job = upload_job(config, members)
    with pytest.raises(MediaError):
        run(job, replace(config, **limits))
    assert Path(job["source"]).exists()
    assert not (config.data_dir / "artifacts" / "upload.cbz").exists()


@pytest.mark.parametrize("name", ["../escape.png", "/absolute.png", "dir/../../escape.png", "C:\\escape.png"])
def test_zip_rejects_unsafe_paths(config, name):
    job = upload_job(config, [(name, PNG)])
    with pytest.raises(MediaError, match="不安全"):
        run(job, config)


def test_zip_symlink_and_corrupt_member_fail(config):
    job = upload_job(config, [])
    with zipfile.ZipFile(job["source"], "w") as archive:
        info = zipfile.ZipInfo("link.png")
        info.create_system = 3
        info.external_attr = (stat.S_IFLNK | 0o777) << 16
        archive.writestr(info, "/etc/passwd")
    with pytest.raises(MediaError, match="链接"):
        run(job, config)
    with zipfile.ZipFile(job["source"], "w", compression=zipfile.ZIP_STORED) as archive:
        archive.writestr("image.png", b"unique payload")
    source = Path(job["source"])
    source.write_bytes(source.read_bytes().replace(b"unique payload", b"broken payload"))
    with pytest.raises(MediaError):
        run(job, config)
    assert not (config.data_dir / "artifacts" / "upload.cbz").exists()


def test_source_and_artifact_boundaries(config, tmp_path):
    job = upload_job(config, [("image.png", PNG)])
    outside = tmp_path / "outside.zip"
    outside.write_bytes(Path(job["source"]).read_bytes())
    Path(job["source"]).unlink()
    Path(job["source"]).symlink_to(outside)
    with pytest.raises(MediaError, match="符号链接"):
        run(job, config)
    with pytest.raises(MediaError):
        run({**job, "source": str(outside)}, config)
    with pytest.raises(MediaError):
        validate_artifact(config, outside)
    link = config.data_dir / "artifacts" / "link.cbz"
    link.symlink_to(outside)
    with pytest.raises(MediaError, match="符号链接"):
        validate_artifact(config, link)
    fifo = config.data_dir / "artifacts" / "fifo.cbz"
    os.mkfifo(fifo)
    with pytest.raises(MediaError, match="普通文件"):
        validate_artifact(config, fifo)
    protected = tmp_path / "protected"
    protected.mkdir()
    sentinel = protected / "keep"
    sentinel.write_text("keep")
    (config.data_dir / "work" / "download").symlink_to(protected, target_is_directory=True)
    with pytest.raises(MediaError, match="符号链接"):
        run(url_job(), fake_config(config))
    assert sentinel.read_text() == "keep"


def test_komga_sanitizes_directories_and_does_not_overwrite(config):
    artifact = run(upload_job(config, [("image.png", PNG)]), config)
    destination = copy_to_komga(config, artifact.path, artifact.filename, "../../")
    assert destination.parent == config.komga_dir / "tankobon"
    destination.write_bytes(b"existing comic")
    with pytest.raises(MediaError, match="不同内容"):
        copy_to_komga(config, artifact.path, artifact.filename, "")
    assert destination.read_bytes() == b"existing comic"
    with pytest.raises(MediaError):
        copy_to_komga(config, artifact.path, "../escape.cbz", "")
    unsafe = config.komga_dir / "unsafe"
    unsafe.symlink_to(config.data_dir, target_is_directory=True)
    with pytest.raises(MediaError, match="符号链接"):
        copy_to_komga(config, artifact.path, artifact.filename, "unsafe")


@pytest.mark.parametrize("replace_ancestor", [False, True])
def test_komga_saved_root_cannot_redirect_through_symlink(config, tmp_path, replace_ancestor):
    artifact = run(upload_job(config, [("image.png", PNG)]), config)
    saved = (tmp_path / "libraries" / "japanese").resolve()
    saved.mkdir(parents=True)
    other = tmp_path / "other-library"
    other.mkdir()
    changed = saved.parent if replace_ancestor else saved
    changed.rename(changed.with_name(changed.name + "-original"))
    if replace_ancestor:
        (other / saved.name).mkdir()
    changed.symlink_to(other, target_is_directory=True)
    with pytest.raises(MediaError, match="符号链接"):
        copy_to_komga(replace(config, komga_dir=saved), artifact.path, artifact.filename, "系列")
    assert not list(other.rglob("*.cbz"))
    assert artifact.path.is_file()


def test_komga_io_failure_preserves_download(config, monkeypatch):
    artifact = run(upload_job(config, [("image.png", PNG)]), config)

    def fail(*args, **kwargs):
        raise PermissionError("fixture permission denial")

    monkeypatch.setattr("telegraph_downloader.media.tempfile.mkstemp", fail)
    with pytest.raises(MediaError, match="复制到 Komga 失败"):
        copy_to_komga(config, artifact.path, artifact.filename, "")
    assert validate_artifact(config, artifact.path) == artifact.path
    assert not list(config.komga_dir.rglob("*.cbz"))


@pytest.mark.parametrize("concurrent_content", ["different", "same", "symlink"])
def test_komga_publication_does_not_clobber_concurrent_writer(config, tmp_path, monkeypatch, concurrent_content):
    artifact = run(upload_job(config, [("image.png", PNG)]), config)
    payload = artifact.path.read_bytes()
    target = config.komga_dir / "tankobon" / artifact.filename
    outside = tmp_path / "existing.cbz"
    outside.write_bytes(b"outside comic")
    original_copy = shutil.copyfileobj

    def concurrent_copy(source, destination, length):
        original_copy(source, destination, length)
        if concurrent_content == "symlink":
            target.symlink_to(outside)
        else:
            target.write_bytes(payload if concurrent_content == "same" else b"concurrent comic")

    monkeypatch.setattr("telegraph_downloader.media.shutil.copyfileobj", concurrent_copy)
    if concurrent_content == "same":
        assert copy_to_komga(config, artifact.path, artifact.filename, "") == target
        assert target.read_bytes() == payload
    else:
        with pytest.raises(MediaError, match="符号链接" if concurrent_content == "symlink" else "不同内容"):
            copy_to_komga(config, artifact.path, artifact.filename, "")
        if concurrent_content == "different":
            assert target.read_bytes() == b"concurrent comic"
        else:
            assert target.is_symlink()
            assert outside.read_bytes() == b"outside comic"
    assert artifact.path.read_bytes() == payload
    assert not list(target.parent.glob(".komga-*.tmp"))


@pytest.mark.parametrize("reason", ["cancel", "timeout", "budget"])
def test_subprocess_is_stopped_and_no_partial_artifact(config, tmp_path, monkeypatch, reason):
    marker = tmp_path / "started.json"
    monkeypatch.setenv("MEDIA_FIXTURE_MARKER", str(marker))
    cancel = threading.Event()
    value = fake_config(config)
    scenario = "hang"
    if reason == "timeout":
        value = replace(value, task_timeout=0.3)
    if reason == "budget":
        value = replace(value, max_image_bytes=1024, max_total_bytes=1024)
        scenario = "budget"

    def request_cancel():
        deadline = time.monotonic() + 3
        while not marker.exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        cancel.set()

    trigger = None
    if reason == "cancel":
        trigger = threading.Thread(target=request_cancel)
        trigger.start()
    started = time.monotonic()
    with pytest.raises(MediaCancelled if reason == "cancel" else MediaError):
        run(url_job(scenario), value, cancel)
    if trigger:
        trigger.join(timeout=3)
    assert time.monotonic() - started < 4
    if marker.exists():
        for pid in json.loads(marker.read_text()).values():
            result = subprocess.run(["ps", "-o", "stat=", "-p", str(pid)], capture_output=True, text=True)
            assert not result.stdout.strip() or result.stdout.strip().startswith("Z")
    assert not (config.data_dir / "artifacts" / "download.cbz").exists()
    assert not (config.data_dir / "work" / "download").exists()


@pytest.mark.parametrize("url", ["http://localhost/page", "https://telegra.ph.evil.test/a", "https://me@telegra.ph/a", "https://telegra.ph:999/a", "file:///tmp/page", "https://telegra.ph/"])
def test_invalid_urls(url):
    with pytest.raises(MediaError):
        canonical_url(url)


def test_graph_alias_is_canonicalized():
    assert canonical_url("http://graph.org/Title-01-01?utm=1#anchor") == "https://telegra.ph/Title-01-01"


@pytest.fixture
def gallery_server(monkeypatch):
    class LocalServer(ThreadingHTTPServer):
        def server_bind(self):
            socketserver.TCPServer.server_bind(self)
            self.server_name = "localhost"
            self.server_port = self.server_address[1]

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            if self.path == "/missing.png":
                self.send_error(404)
                return
            if self.path.endswith(".png"):
                body = PNG
                content_type = "image/png"
            else:
                middle = "/missing.png" if self.path == "/partial" else "/2.png"
                body = (
                    '<meta property="og:title" content="Offline">'
                    '<link rel="canonical" href="https://telegra.ph/test">'
                    '<article><figure><img src="/1.png"></figure>'
                    f'<figure><img src="{middle}"></figure>'
                    '<figure><img src="/3.png"></figure></article>'
                ).encode()
                content_type = "text/html"
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    server = LocalServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever)
    thread.start()
    monkeypatch.setenv("GALLERY_FIXTURE_ROOT", f"http://127.0.0.1:{server.server_port}")
    yield server
    server.shutdown()
    server.server_close()
    thread.join()


@pytest.mark.parametrize("scenario", ["ok", "partial"])
def test_real_gallery_offline(config, gallery_server, scenario):
    pytest.importorskip("gallery_dl", reason="gallery-dl must be installed for the offline extractor smoke test")
    value = replace(config, gallery_command=(sys.executable, str(FIXTURES / "gallery_runner.py")))
    if scenario == "partial":
        with pytest.raises(MediaError):
            run(url_job(scenario), value)
        assert not (config.data_dir / "artifacts" / "download.cbz").exists()
    else:
        artifact = run(url_job(scenario), value)
        with zipfile.ZipFile(artifact.path) as archive:
            assert artifact.page_count == 3
            for number in range(1, 4):
                assert archive.read(f"{number:05d}.png") == PNG


def test_real_7z_stream(config, tmp_path):
    if not shutil.which("bsdtar"):
        pytest.skip("bsdtar/libarchive is required for the actual 7Z streaming integration test")
    job = upload_job(config, [], suffix=".7z")
    (tmp_path / "2.jpg").write_bytes(b"second")
    (tmp_path / "1.png").write_bytes(PNG)
    subprocess.run(["bsdtar", "--format=7zip", "-cf", job["source"], "-C", str(tmp_path), "2.jpg", "1.png"], check=True)
    artifact = run(job, config)
    with zipfile.ZipFile(artifact.path) as archive:
        assert archive.read("00001.png") == PNG
        assert archive.read("00002.jpg") == b"second"


@pytest.mark.parametrize("unsafe", ["symlink", "total", "path"])
def test_tar_stream_members_are_bounded(config, tmp_path, monkeypatch, unsafe):
    job = upload_job(config, [], suffix=".rar")
    with tarfile.open(job["source"], "w") as archive:
        info = tarfile.TarInfo("../escape.png" if unsafe == "path" else "image.png")
        if unsafe == "symlink":
            info.type = tarfile.SYMTYPE
            info.linkname = "/etc/passwd"
            archive.addfile(info)
        else:
            info.size = len(PNG)
            archive.addfile(info, io.BytesIO(PNG))
            if unsafe == "total":
                other = tarfile.TarInfo("hidden.txt")
                other.size = 500
                archive.addfile(other, io.BytesIO(b"x" * 500))
    tool = tmp_path / "bin"
    tool.mkdir()
    executable = tool / "bsdtar"
    executable.write_text(f"#!{sys.executable}\nimport pathlib,sys\nsys.stdout.buffer.write(pathlib.Path(sys.argv[-1][1:]).read_bytes())\n")
    executable.chmod(0o755)
    monkeypatch.setenv("PATH", str(tool) + os.pathsep + os.environ.get("PATH", ""))
    value = replace(config, max_total_bytes=100) if unsafe == "total" else config
    with pytest.raises(MediaError):
        run(job, value)
    assert not (config.data_dir / "artifacts" / "upload.cbz").exists()


def test_cancellation_unblocks_stalled_tar_reader(config, tmp_path, monkeypatch):
    job = upload_job(config, [], suffix=".rar")
    Path(job["source"]).write_bytes(b"fixture")
    tool = tmp_path / "bin"
    tool.mkdir()
    marker = tmp_path / "started"
    executable = tool / "bsdtar"
    executable.write_text(
        f"#!{sys.executable}\nimport pathlib,sys,tarfile,time\n"
        "header=tarfile.TarInfo('image.png');header.size=1024\n"
        "sys.stdout.buffer.write(header.tobuf()+b'x');sys.stdout.buffer.flush()\n"
        f"pathlib.Path({str(marker)!r}).write_text('ready')\ntime.sleep(60)\n"
    )
    executable.chmod(0o755)
    monkeypatch.setenv("PATH", str(tool) + os.pathsep + os.environ.get("PATH", ""))
    cancel = threading.Event()

    def stop():
        deadline = time.monotonic() + 3
        while not marker.exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        cancel.set()

    trigger = threading.Thread(target=stop)
    trigger.start()
    with pytest.raises(MediaCancelled):
        run(job, config, cancel)
    trigger.join(timeout=3)
    assert not (config.data_dir / "artifacts" / "upload.cbz").exists()

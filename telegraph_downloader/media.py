from __future__ import annotations

import contextlib
import hashlib
import json
import os
import re
import shutil
import signal
import stat
import subprocess
import tarfile
import tempfile
import threading
import time
import zipfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import TYPE_CHECKING, BinaryIO, Callable, Iterator
from urllib.parse import urlsplit
from xml.etree import ElementTree

if TYPE_CHECKING:
    from .config import Config

IMAGE_EXTENSIONS = {".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif"}
XML_FIELDS = {
    "author": "Writer",
    "series_name": "Series",
    "series_number": "Number",
    "comic_name": "Title",
    "summary": "Summary",
    "tags": "Tags",
    "genres": "Genre",
}
_INVALID_XML = re.compile(
    "[^\x09\x0a\x0d\x20-\ud7ff\ue000-\ufffd\U00010000-\U0010ffff]"
)
_COPY_LOCK = threading.Lock()
_CHUNK_SIZE = 64 * 1024


class MediaError(Exception):
    pass


class MediaCancelled(MediaError):
    pass


@dataclass(frozen=True)
class Artifact:
    path: Path
    filename: str
    page_count: int
    size: int


def canonical_url(value: str) -> str:
    try:
        url = urlsplit(value.strip())
        if (
            url.scheme not in {"http", "https"}
            or url.hostname not in {"telegra.ph", "graph.org"}
            or url.username
            or url.password
            or url.port is not None
            or not url.path.strip("/")
            or "/" in url.path.strip("/")
            or any(ord(char) < 32 for char in value)
        ):
            raise ValueError
    except (ValueError, AttributeError) as exc:
        raise MediaError("请输入有效的 Telegraph 页面链接") from exc
    return "https://telegra.ph/" + url.path.strip("/")


def _inside(root: Path, path: Path) -> Path:
    path = Path(path).absolute()
    try:
        relative = path.relative_to(root)
    except ValueError as exc:
        raise MediaError("文件路径超出允许目录") from exc
    if ".." in relative.parts:
        raise MediaError("文件路径包含非法目录")
    current = root
    for part in relative.parts:
        current /= part
        if current.is_symlink():
            raise MediaError("不允许使用符号链接")
    if root.is_symlink():
        raise MediaError("不允许使用符号链接目录")
    return path


def _directory(root: Path, path: Path) -> Path:
    path = _inside(root, path)
    path.mkdir(parents=True, exist_ok=True)
    return path


def _open_regular(root: Path, path: Path) -> BinaryIO:
    path = _inside(root, path)
    descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | getattr(os, "O_NOFOLLOW", 0))
    if not stat.S_ISREG(os.fstat(descriptor).st_mode):
        os.close(descriptor)
        raise MediaError("文件不是普通文件")
    return os.fdopen(descriptor, "rb")


def validate_artifact(config: Config, path: str | Path) -> Path:
    root = config.data_dir.resolve()
    path = _inside(root, Path(path))
    artifacts = _inside(root, root / "artifacts")
    if path.parent != artifacts or path.suffix != ".cbz":
        raise MediaError("产物路径无效")
    try:
        with _open_regular(root, path) as source:
            if os.fstat(source.fileno()).st_size <= 0:
                raise MediaError("产物文件为空")
    except OSError as exc:
        raise MediaError("产物文件不可用") from exc
    return path


def _check(cancel: threading.Event, deadline: float) -> None:
    if cancel.is_set():
        raise MediaCancelled("任务已取消")
    if time.monotonic() >= deadline:
        raise MediaError("任务处理超时")


def _terminate(process: subprocess.Popen) -> None:
    with contextlib.suppress(ProcessLookupError):
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=1)
    except subprocess.TimeoutExpired:
        pass
    with contextlib.suppress(ProcessLookupError):
        os.killpg(process.pid, signal.SIGKILL)
    process.wait()


@contextlib.contextmanager
def _command(
    argv: list[str],
    work: Path,
    config: Config,
    cancel: threading.Event,
    deadline: float,
    *,
    stream: bool = False,
) -> Iterator[subprocess.Popen]:
    _check(cancel, deadline)
    stopped = threading.Event()
    failures: list[MediaError] = []
    with tempfile.NamedTemporaryFile(prefix=".tool-", dir=work) as errors:
        try:
            process = subprocess.Popen(
                argv,
                cwd=work,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE if stream else subprocess.DEVNULL,
                stderr=errors,
                start_new_session=True,
            )
        except OSError as exc:
            raise MediaError("无法启动媒体处理工具") from exc
        terminate_lock = threading.Lock()
        terminated = False

        def stop_process() -> None:
            nonlocal terminated
            with terminate_lock:
                if not terminated:
                    _terminate(process)
                    terminated = True

        def watch() -> None:
            while not stopped.wait(0.05):
                if process.poll() is not None:
                    return
                try:
                    _check(cancel, deadline)
                    total = 0
                    for path in work.rglob("*"):
                        if path.is_symlink():
                            raise MediaError("处理目录中出现符号链接")
                        if path.is_file():
                            total += path.stat().st_size
                    # shortcut: polling bounds temporary storage approximately; exact quotas belong to the filesystem.
                    if total > config.max_total_bytes + config.max_image_bytes:
                        raise MediaError("临时文件超过任务容量限制")
                except MediaError as exc:
                    failures.append(exc)
                    stop_process()
                    return
                except FileNotFoundError:
                    continue
                except OSError:
                    failures.append(MediaError("无法检查临时文件容量"))
                    stop_process()
                    return

        watcher = threading.Thread(target=watch, daemon=True)
        watcher.start()
        body_error = None
        try:
            yield process
            process.wait()
        except BaseException as exc:
            body_error = exc
        finally:
            stop_process()
            stopped.set()
            watcher.join()
            if process.stdout:
                process.stdout.close()
        if failures:
            raise failures[0]
        _check(cancel, deadline)
        if body_error:
            raise body_error
        if process.returncode:
            raise MediaError(f"媒体处理工具执行失败（退出码 {process.returncode}）")


def _download(
    url: str, work: Path, config: Config, cancel: threading.Event, deadline: float
) -> list[Path]:
    images_dir = _directory(work, work / "images")
    argv = [
        *config.gallery_command,
        "--config-ignore",
        "--no-input",
        "--whitelist",
        "telegraph",
        "--write-info-json",
        "--range",
        f"1-{config.max_images}",
        "--http-timeout",
        str(min(config.task_timeout, 30)),
        "--retries",
        "2",
        "-D",
        str(images_dir),
        "-f",
        "{num:05d}.{extension}",
        canonical_url(url),
    ]
    with _command(argv, work, config, cancel, deadline):
        pass
    try:
        with _open_regular(work, images_dir / "info.json") as source:
            if os.fstat(source.fileno()).st_size > 1024 * 1024:
                raise MediaError("下载器图片清单超过容量限制")
            manifest = json.load(source)
        count = manifest["count"]
    except (OSError, ValueError, KeyError, TypeError) as exc:
        raise MediaError("下载器未生成有效的图片清单") from exc
    if type(count) is not int or not 1 <= count <= config.max_images:
        raise MediaError("页面图片数量为空或超过限制")
    images = []
    for path in sorted(images_dir.iterdir()):
        if path.name == "info.json":
            continue
        if path.suffix.lower() not in IMAGE_EXTENSIONS or path.stem != f"{len(images) + 1:05d}":
            raise MediaError("下载结果包含缺页或非图片文件")
        images.append(path)
    if len(images) != count:
        raise MediaError("下载图片不完整，未生成 CBZ")
    total = 0
    for path in images:
        with _open_regular(work, path) as source:
            size = os.fstat(source.fileno()).st_size
        if not 0 < size <= config.max_image_bytes:
            raise MediaError("单张图片为空或超过容量限制")
        total += size
    if total > config.max_total_bytes:
        raise MediaError("图片总容量超过限制")
    return images


def _copy_member(
    source: BinaryIO,
    target: BinaryIO | None,
    config: Config,
    totals: list[int],
    cancel: threading.Event,
    deadline: float,
) -> int:
    size = 0
    while True:
        _check(cancel, deadline)
        chunk = source.read(_CHUNK_SIZE)
        if not chunk:
            break
        size += len(chunk)
        totals[0] += len(chunk)
        if totals[0] > config.max_total_bytes:
            raise MediaError("压缩包解压总容量超过限制")
        if target:
            if size > config.max_image_bytes:
                raise MediaError("压缩包单张图片超过容量限制")
            target.write(chunk)
    return size


def _read_archive(
    source: Path, work: Path, config: Config, cancel: threading.Event, deadline: float
) -> list[Path]:
    images_dir = _directory(work, work / "images")
    images: list[tuple[str, Path]] = []
    totals = [0]
    declared_total = 0

    def member(name: str, size: int, reader: Callable[[], BinaryIO]) -> None:
        nonlocal declared_total
        _check(cancel, deadline)
        declared_total += size
        if size < 0 or declared_total > config.max_total_bytes:
            raise MediaError("压缩包解压总容量超过限制")
        suffix = Path(name).suffix.lower()
        is_image = suffix in IMAGE_EXTENSIONS
        if is_image and (len(images) >= config.max_images or not 0 < size <= config.max_image_bytes):
            raise MediaError("压缩包图片数量或单图容量超过限制")
        with reader() as contents:
            if is_image:
                path = images_dir / f"{len(images) + 1:05d}{suffix}"
                with path.open("xb") as target:
                    actual = _copy_member(contents, target, config, totals, cancel, deadline)
                images.append((name, path))
            else:
                actual = _copy_member(contents, None, config, totals, cancel, deadline)
        if actual != size:
            raise MediaError("压缩包成员数据不完整")

    if source.suffix.lower() == ".zip":
        with _open_regular(config.data_dir.resolve(), source) as upload, zipfile.ZipFile(upload) as archive:
            for info in archive.infolist():
                _safe_member_name(info.filename)
                kind = stat.S_IFMT(info.external_attr >> 16)
                if kind not in {0, stat.S_IFREG, stat.S_IFDIR}:
                    raise MediaError("压缩包包含链接或特殊文件")
                if info.is_dir():
                    continue
                if kind == stat.S_IFDIR or info.flag_bits & 1:
                    raise MediaError("不支持加密或异常压缩包成员")
                member(info.filename, info.file_size, lambda info=info: archive.open(info))
    else:
        with _command(
            ["bsdtar", "-cf", "-", "--format=pax", "@" + str(source)],
            work,
            config,
            cancel,
            deadline,
            stream=True,
        ) as process:
            with tarfile.open(fileobj=process.stdout, mode="r|") as archive:
                for info in archive:
                    _safe_member_name(info.name)
                    if info.isdir():
                        continue
                    if not info.isreg():
                        raise MediaError("压缩包包含链接或特殊文件")
                    member(info.name, info.size, lambda info=info: archive.extractfile(info))
    if not images:
        raise MediaError("压缩包中没有图片")
    return [path for _, path in sorted(images, key=lambda item: item[0])]


def _safe_member_name(name: str) -> None:
    path = PurePosixPath(name.replace("\\", "/"))
    if path.is_absolute() or ".." in path.parts or "\x00" in name or (path.parts and ":" in path.parts[0]):
        raise MediaError("压缩包包含不安全的成员路径")


def _segment(value: str, fallback: str = "") -> str:
    clean = re.sub(r'[<>:"/\\|?*\x00-\x1f]', " ", _INVALID_XML.sub("", value))
    clean = " ".join(clean.split()).strip(" .")
    return clean.encode("utf-8")[:100].decode("utf-8", errors="ignore").strip(" .") or fallback


def _filename(metadata: dict) -> str:
    parts = [_segment(metadata.get("author", ""), "未知作者")]
    if series := _segment(metadata.get("series_name", "")):
        parts.append(series)
    parts.append(_segment(metadata.get("comic_name", ""), "未命名漫画"))
    suffix = f"_{int(time.time())}.cbz"
    body = "_".join(parts).encode("utf-8")[:244 - len(suffix)].decode("utf-8", errors="ignore")
    return body.rstrip(" .") + suffix


def _package(
    images: list[Path], metadata: dict, output: Path, config: Config,
    cancel: threading.Event, deadline: float,
) -> None:
    comic = ElementTree.Element("ComicInfo")
    for field, tag in XML_FIELDS.items():
        ElementTree.SubElement(comic, tag).text = _INVALID_XML.sub("", metadata.get(field, "").strip())
    descriptor, temporary = tempfile.mkstemp(prefix=f".{output.stem}-", suffix=".tmp", dir=output.parent)
    os.close(descriptor)
    try:
        with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_STORED) as archive:
            archive.writestr("ComicInfo.xml", ElementTree.tostring(comic, encoding="utf-8", xml_declaration=True))
            for number, path in enumerate(images, 1):
                with _open_regular(config.data_dir.resolve(), path) as source:
                    with archive.open(f"{number:05d}{path.suffix.lower()}", "w") as target:
                        while True:
                            _check(cancel, deadline)
                            chunk = source.read(_CHUNK_SIZE)
                            if not chunk:
                                break
                            target.write(chunk)
        _check(cancel, deadline)
        os.chmod(temporary, 0o644)
        os.replace(temporary, output)
    finally:
        Path(temporary).unlink(missing_ok=True)


def execute(
    job: dict, config: Config, cancel: threading.Event, progress: Callable[[str], None]
) -> Artifact:
    task_id = job["id"]
    if not isinstance(task_id, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,80}", task_id):
        raise MediaError("任务编号无效")
    metadata = job.get("metadata", {})
    if not isinstance(metadata, dict) or any(not isinstance(metadata.get(key, ""), str) for key in XML_FIELDS):
        raise MediaError("漫画元数据无效")
    root = config.data_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    work = _inside(root, root / "work" / task_id)
    output = _inside(root, root / "artifacts" / f"{task_id}.cbz")
    deadline = time.monotonic() + config.task_timeout
    try:
        _check(cancel, deadline)
        if work.exists():
            shutil.rmtree(work)
        _directory(root, work)
        _directory(root, output.parent)
        if output.exists():
            output.unlink()
        if job["kind"] == "url":
            progress("下载图片")
            images = _download(job["source"], work, config, cancel, deadline)
        elif job["kind"] == "upload":
            progress("读取压缩包")
            source = _inside(root, Path(job["source"]))
            if source.parent != root / "inputs" / task_id or source.suffix.lower() not in {".zip", ".rar", ".7z"}:
                raise MediaError("上传源文件路径无效")
            with _open_regular(root, source) as upload:
                if os.fstat(upload.fileno()).st_size > config.max_upload_bytes:
                    raise MediaError("上传源文件超过容量限制")
            images = _read_archive(source, work, config, cancel, deadline)
        else:
            raise MediaError("不支持的任务类型")
        progress("生成 CBZ")
        _package(images, metadata, output, config, cancel, deadline)
        _check(cancel, deadline)
        return Artifact(output, _filename(metadata), len(images), output.stat().st_size)
    except MediaError:
        output.unlink(missing_ok=True)
        raise
    except (OSError, ValueError, RuntimeError, zipfile.BadZipFile, tarfile.TarError) as exc:
        output.unlink(missing_ok=True)
        raise MediaError("媒体文件处理失败，请检查源文件与目录权限") from exc
    finally:
        if work.exists():
            shutil.rmtree(work, ignore_errors=True)


def copy_to_komga(config: Config, path: str | Path, filename: str, series_name: str) -> Path:
    if config.komga_dir is None:
        raise MediaError("尚未配置 Komga 目录")
    source = validate_artifact(config, path)
    if Path(filename).name != filename or not filename.endswith(".cbz") or "\\" in filename:
        raise MediaError("产物文件名无效")
    try:
        root = config.komga_dir.resolve()
        directory = _directory(root, root / _segment(series_name, "tankobon"))
        target = _inside(root, directory / filename)
    except OSError as exc:
        raise MediaError("无法访问 Komga 目录，请检查目录权限") from exc

    def same_content() -> bool:
        try:
            with _open_regular(config.data_dir.resolve(), source) as left, _open_regular(root, target) as right:
                return hashlib.file_digest(left, "sha256").digest() == hashlib.file_digest(right, "sha256").digest()
        except OSError as exc:
            raise MediaError("无法读取已有 Komga 文件") from exc

    with _COPY_LOCK:
        if target.exists():
            if same_content():
                return target
            raise MediaError("Komga 中已存在不同内容的同名文件")
        temporary = None
        try:
            descriptor, temporary = tempfile.mkstemp(prefix=".komga-", suffix=".tmp", dir=directory)
            with os.fdopen(descriptor, "wb") as output, _open_regular(config.data_dir.resolve(), source) as source_file:
                shutil.copyfileobj(source_file, output, _CHUNK_SIZE)
            _inside(root, target)
            os.chmod(temporary, 0o644)
            try:
                os.link(temporary, target)
            except FileExistsError:
                if not same_content():
                    raise MediaError("Komga 中已存在不同内容的同名文件") from None
        except OSError as exc:
            raise MediaError("复制到 Komga 失败，请检查目录权限") from exc
        finally:
            if temporary:
                Path(temporary).unlink(missing_ok=True)
    return target

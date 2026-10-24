from __future__ import annotations

import json
import os
import shutil
import uuid
from contextlib import asynccontextmanager
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

from fastapi import FastAPI, HTTPException, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from pydantic import BaseModel, ConfigDict, Field, ValidationError
from starlette.datastructures import UploadFile

from .config import Config
from .jobs import Runner
from .media import MediaError, canonical_url, execute, validate_artifact
from .store import Store

PACKAGE = Path(__file__).parent
STATUS_LABELS = {
    "queued": "排队中", "running": "处理中", "delivering": "入库中", "succeeded": "已入库",
    "failed": "处理失败", "cancelled": "已取消",
}


class Metadata(BaseModel):
    model_config = ConfigDict(extra="forbid")
    author: str = Field(default="", max_length=200)
    series_name: str = Field(default="", max_length=200)
    series_number: str = Field(default="", max_length=50)
    comic_name: str = Field(default="", max_length=200)
    summary: str = Field(default="", max_length=10000)
    tags: str = Field(default="", max_length=2000)
    genres: str = Field(default="", max_length=2000)


class URLInput(Metadata):
    url: str = Field(min_length=1, max_length=2048)


class KomgaSettings(BaseModel):
    model_config = ConfigDict(extra="forbid")
    komga_dir: str = Field(min_length=1, max_length=4096)
    komga_url: str = Field(default="", max_length=2048)


class RequestGuard:
    def __init__(self, app, max_body: int):
        self.app, self.max_body = app, max_body

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http" or scope["method"] in {"GET", "HEAD", "OPTIONS"}:
            return await self.app(scope, receive, send)
        headers = dict(scope.get("headers", []))
        origin = headers.get(b"origin", b"").decode("latin1")
        host = headers.get(b"host", b"").decode("latin1")
        try:
            origin_host = urlsplit(origin).netloc
        except ValueError:
            origin_host = ""
        if (origin and origin_host != host) or headers.get(b"sec-fetch-site") == b"cross-site":
            return await JSONResponse({"detail": "请从本应用页面提交操作"}, 403)(scope, receive, send)
        try:
            length = int(headers.get(b"content-length", b"0"))
        except ValueError:
            return await JSONResponse({"detail": "请求长度无效"}, 400)(scope, receive, send)
        limit = self.max_body if scope["path"] == "/api/jobs/upload" else 128 * 1024
        if length > limit:
            return await JSONResponse({"detail": "上传内容过大，请缩小文件后重试"}, 413)(scope, receive, send)
        received = 0

        async def bounded_receive():
            nonlocal received
            message = await receive()
            received += len(message.get("body", b""))
            if received > limit:
                raise HTTPException(413, "上传内容过大，请缩小文件后重试")
            return message

        await self.app(scope, bounded_receive, send)


def create_app(config: Config | None = None, executor=execute) -> FastAPI:
    config = config or Config()
    store = Store(config.data_dir)
    runner = Runner(store, config, executor)

    @asynccontextmanager
    async def lifespan(app):
        await runner.start()
        try:
            yield
        finally:
            await runner.stop()

    app = FastAPI(title="墨渡 PanelPort", lifespan=lifespan, docs_url=None, redoc_url=None)
    app.state.config, app.state.store, app.state.runner = config, store, runner
    app.add_middleware(RequestGuard, max_body=config.max_upload_bytes + 128 * 1024)
    app.mount("/static", StaticFiles(directory=PACKAGE / "static", check_dir=False), name="static")
    templates = Jinja2Templates(directory=PACKAGE / "templates")

    @app.exception_handler(RequestValidationError)
    async def invalid_request(request, exc):
        return JSONResponse({"detail": "填写内容不完整或过长，请检查后重试"}, 422)

    def current_settings() -> dict:
        value = store.settings(str(config.komga_dir or ""))
        value["configured"] = bool(value["komga_dir"] and Path(value["komga_dir"]).is_dir())
        return value

    def require_destination() -> None:
        if not current_settings()["configured"]:
            raise HTTPException(409, "请先在 Komga 设置中配置共享书库目录")

    def require_job(job_id: str) -> dict:
        job = store.get(job_id)
        if job is None:
            raise HTTPException(404, "任务不存在")
        return job

    def source_available(job: dict) -> bool:
        if job.get("artifact_path"):
            try:
                validate_artifact(config, Path(job["artifact_path"]))
                return True
            except (MediaError, OSError):
                pass
        if job["kind"] == "url":
            return True
        path = Path(job["source"])
        root = config.data_dir.resolve() / "inputs" / job["id"]
        return (not root.is_symlink() and not path.is_symlink()
                and path.is_file() and path.resolve().parent == root)

    def public(job: dict) -> dict:
        result = {key: job[key] for key in (
            "id", "kind", "metadata", "status", "phase", "error", "cancel_requested",
            "filename", "page_count", "size", "created_at", "updated_at",
        )}
        result["status_label"] = STATUS_LABELS.get(job["status"], "未知状态")
        result["source_label"] = "Telegraph 图集" if job["kind"] == "url" else "上传压缩包"
        actions = []
        if job["status"] in {"queued", "running"} and not job["cancel_requested"]:
            actions.append("cancel")
        if job["status"] in {"failed", "cancelled"} and source_available(job):
            actions.append("retry")
        result["available_actions"] = actions
        return result

    async def payload(request: Request) -> dict:
        try:
            if request.headers.get("content-type", "").split(";")[0] == "application/json":
                value = await request.json()
            else:
                value = dict(await request.form(max_files=0, max_fields=12, max_part_size=65536))
            if not isinstance(value, dict):
                raise ValueError
            return value
        except (ValueError, json.JSONDecodeError):
            raise HTTPException(400, "请求内容无效，请重新提交") from None

    async def page(request: Request, name: str):
        settings = current_settings()
        return templates.TemplateResponse(request=request, name=f"{name}.html", context={
            "active_page": "home" if name == "index" else name,
            "settings": settings, "komga_enabled": settings["configured"],
            "jobs": [public(job) for job in store.list()] if name == "tasks" else [],
        })

    @app.get("/")
    async def index(request: Request):
        return await page(request, "index")

    @app.get("/tasks")
    async def tasks_page(request: Request):
        return await page(request, "tasks")

    @app.get("/settings")
    async def settings_page(request: Request):
        return await page(request, "settings")

    @app.get("/healthz")
    async def health():
        if not runner.task or runner.task.done():
            raise HTTPException(503, "任务执行器未就绪")
        return {"ok": True}

    @app.get("/api/jobs")
    async def list_jobs():
        return {"jobs": [public(job) for job in store.list()]}

    @app.get("/api/jobs/{job_id}")
    async def get_job(job_id: str):
        return public(require_job(job_id))

    @app.post("/api/jobs/url", status_code=202)
    async def submit_url(request: Request):
        require_destination()
        try:
            data = URLInput.model_validate(await payload(request))
        except ValidationError:
            raise HTTPException(422, "请检查链接和元数据的填写长度") from None
        try:
            url = canonical_url(data.url)
        except MediaError as exc:
            raise HTTPException(400, str(exc)) from exc
        job = store.create("url", url, data.model_dump(exclude={"url"}))
        runner.notify()
        return public(job)

    @app.post("/api/jobs/upload", status_code=202)
    async def submit_upload(request: Request):
        require_destination()
        async with request.form(max_files=1, max_fields=7, max_part_size=65536) as form:
            upload = form.get("file")
            if not isinstance(upload, UploadFile):
                raise HTTPException(400, "请选择 ZIP、RAR 或 7Z 压缩包")
            extension = Path(upload.filename or "").suffix.lower()
            if extension not in {".zip", ".rar", ".7z"}:
                raise HTTPException(400, "仅支持 ZIP、RAR 和 7Z 压缩包")
            try:
                metadata = Metadata.model_validate({key: value for key, value in form.items() if key != "file"})
            except ValidationError:
                raise HTTPException(422, "请检查元数据的填写长度") from None
            job_id = uuid.uuid4().hex
            source_dir = config.data_dir / "inputs" / job_id
            source_dir.mkdir()
            source = source_dir / f"source{extension}"
            try:
                total = 0
                with source.open("xb") as target:
                    while chunk := await upload.read(64 * 1024):
                        total += len(chunk)
                        if total > config.max_upload_bytes:
                            raise HTTPException(413, "压缩包超过上传大小限制")
                        target.write(chunk)
                if not total:
                    raise HTTPException(400, "压缩包为空，请重新选择文件")
                job = store.create("upload", str(source.resolve()), metadata.model_dump(), job_id)
            except BaseException:
                shutil.rmtree(source_dir)
                raise
        runner.notify()
        return public(job)

    @app.post("/api/jobs/{job_id}/cancel")
    async def cancel_job(job_id: str):
        require_job(job_id)
        try:
            return public(runner.cancel(job_id))
        except ValueError as exc:
            raise HTTPException(409, str(exc)) from exc

    @app.post("/api/jobs/{job_id}/retry")
    async def retry_job(job_id: str):
        job = require_job(job_id)
        if not source_available(job):
            raise HTTPException(409, "原始文件已不可用，请重新上传")
        try:
            result = store.retry(job_id)
        except ValueError as exc:
            raise HTTPException(409, str(exc)) from exc
        runner.notify()
        return public(result)

    @app.get("/api/settings")
    async def get_settings():
        return current_settings()

    @app.post("/api/settings")
    async def save_settings(request: Request):
        try:
            value = KomgaSettings.model_validate(await payload(request))
        except ValidationError:
            raise HTTPException(422, "请填写有效的 Komga 地址和共享书库目录") from None
        directory = Path(value.komga_dir.strip())
        try:
            writable = (directory.is_absolute() and directory.is_dir()
                        and os.access(directory, os.W_OK | os.X_OK))
        except (OSError, ValueError):
            writable = False
        if not writable:
            raise HTTPException(400, "共享书库目录必须是本应用已挂载、可写的绝对目录")
        url = value.komga_url.strip()
        if url:
            try:
                parsed = urlsplit(url)
                if (parsed.scheme not in {"http", "https"} or not parsed.hostname
                        or parsed.username or parsed.password or parsed.query or parsed.fragment
                        or any(ord(char) < 32 for char in url)):
                    raise ValueError
                parsed.port
                url = urlunsplit((parsed.scheme, parsed.netloc, parsed.path.rstrip("/"), "", ""))
            except ValueError:
                raise HTTPException(400, "Komga 网页地址应为不含账号密码的 HTTP 或 HTTPS 地址") from None
        store.save_settings(str(directory.resolve()), url)
        return current_settings()

    return app


app = create_app()

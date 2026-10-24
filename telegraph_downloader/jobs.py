from __future__ import annotations

import asyncio
import fcntl
import logging
import threading
from dataclasses import replace
from pathlib import Path

from .config import Config
from .media import Artifact, MediaCancelled, MediaError, copy_to_komga, execute, validate_artifact
from .store import Store

logger = logging.getLogger(__name__)


class Runner:
    def __init__(self, store: Store, config: Config, executor=execute):
        self.store, self.config, self.executor = store, config, executor
        self.wakeup = asyncio.Event()
        self.stopping = False
        self.active: dict[str, threading.Event] = {}
        self.task: asyncio.Task | None = None
        self.lock = None

    async def start(self) -> None:
        self.config.prepare()
        lock_path = self.config.data_dir / ".instance.lock"
        if lock_path.is_symlink():
            raise RuntimeError("实例锁不能是符号链接")
        self.lock = lock_path.open("a+")
        try:
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as exc:
            self.lock.close()
            raise RuntimeError("此数据目录已有运行实例，请只启动一个进程") from exc
        try:
            self.store.initialize(str(self.config.komga_dir or ""))
            self.store.recover()
            self.task = asyncio.create_task(self.run(), name="comic-archive-runner")
        except BaseException:
            self.lock.close()
            raise

    async def stop(self) -> None:
        self.stopping = True
        for cancel in self.active.values():
            cancel.set()
        self.wakeup.set()
        try:
            if self.task:
                await self.task
        finally:
            if self.lock:
                self.lock.close()

    def notify(self) -> None:
        self.wakeup.set()

    def cancel(self, job_id: str) -> dict:
        result = self.store.cancel(job_id)
        if job_id in self.active:
            self.active[job_id].set()
        return result

    async def run(self) -> None:
        while not self.stopping:
            self.wakeup.clear()
            job = self.store.claim()
            if job:
                await self.process(job)
                continue
            try:
                await asyncio.wait_for(self.wakeup.wait(), timeout=1)
            except TimeoutError:
                pass

    async def process(self, job: dict) -> None:
        cancel = threading.Event()
        self.active[job["id"]] = cancel
        if self.store.get(job["id"])["cancel_requested"]:
            cancel.set()
        artifact = None
        try:
            saved = job.get("destination", {})
            destination = Path(saved["path"]) if saved.get("path") else None
            if destination is None or not destination.is_dir() or destination.resolve() != destination:
                raise MediaError("任务原目标书库不可用，请恢复原目录；没有目标的旧任务请重新提交")
            if job.get("artifact_path"):
                try:
                    cached = validate_artifact(self.config, job["artifact_path"])
                    artifact = Artifact(cached, job["filename"], job["page_count"], job["size"])
                except MediaError:
                    pass
            if artifact is None:
                artifact = await asyncio.to_thread(
                    self.executor, job, self.config, cancel,
                    lambda phase: self.store.progress(job["id"], phase),
                )
            if cancel.is_set() or not self.store.begin_delivery(job["id"], artifact):
                artifact.path.unlink(missing_ok=True)
                self.store.finish(job["id"], "failed" if self.stopping else "cancelled",
                                  "服务停止中断，请重试" if self.stopping else None)
                return
            # Delivery is a short commit phase: cancellation is closed before publishing outside DATA_DIR.
            if not destination.is_dir() or destination.resolve() != destination:
                raise MediaError("任务原目标书库路径已改变，请恢复原目录后重试")
            target = await asyncio.to_thread(
                copy_to_komga, replace(self.config, komga_dir=destination), artifact.path,
                artifact.filename, job["metadata"].get("series_name", ""),
            )
            if not self.store.complete(job["id"], target):
                raise MediaError("入库结果尚未登记，请重试确认")
            try:
                artifact.path.unlink(missing_ok=True)
                source = self.config.data_dir / "inputs" / job["id"]
                if job["kind"] == "upload" and source.exists():
                    for file in source.iterdir():
                        file.unlink(missing_ok=True)
                    source.rmdir()
            except OSError:
                logger.warning("Delivered task temporary cleanup failed: %s", job["id"])
        except MediaCancelled:
            self.store.finish(job["id"], "failed" if self.stopping else "cancelled",
                              "服务停止中断，请重试" if self.stopping else None)
        except MediaError as exc:
            self.store.finish(job["id"], "failed", str(exc))
        except Exception:
            logger.error("Task execution failed: %s", job["id"])
            self.store.finish(job["id"], "failed", "处理未完成，请重试或检查文件")
        finally:
            self.active.pop(job["id"], None)

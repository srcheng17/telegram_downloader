import threading
from concurrent.futures import ThreadPoolExecutor

from telegram_downloader.services.download_worker import execute_download_task


class TaskOrchestrator:
    """Coordinates task execution and worker pool lifecycle."""

    def __init__(
        self,
        task_store,
        logger,
        initial_task_concurrency,
        execution_backend="thread",
        celery_app=None,
    ):
        self._task_store = task_store
        self._logger = logger
        self._executor_lock = threading.Lock()
        self._execution_backend = (execution_backend or "thread").strip().lower()
        self._celery_app = celery_app
        self._executor = (
            ThreadPoolExecutor(max_workers=initial_task_concurrency)
            if self._execution_backend == "thread"
            else None
        )

    def _resolve_celery_app(self):
        if self._celery_app is not None:
            return self._celery_app
        from telegram_downloader.celery_app import celery_app

        self._celery_app = celery_app
        return self._celery_app

    def submit_download(self, task_id, url, timeout, retries, image_concurrency):
        if self._execution_backend == "celery":
            celery_app = self._resolve_celery_app()
            celery_app.send_task(
                "telegram_downloader.download_task",
                kwargs={
                    "task_id": task_id,
                    "url": url,
                    "timeout": timeout,
                    "retries": retries,
                    "image_concurrency": image_concurrency,
                },
            )
            return

        with self._executor_lock:
            self._executor.submit(
                self._run_download,
                task_id,
                url,
                timeout,
                retries,
                image_concurrency,
            )

    def update_task_concurrency(self, new_task_concurrency):
        """Updates the worker count used to process download tasks."""
        if self._execution_backend != "thread":
            self._logger.info(
                "Ignored task_concurrency=%s update for %s backend.",
                new_task_concurrency,
                self._execution_backend,
            )
            return
        with self._executor_lock:
            if self._executor._max_workers == new_task_concurrency:
                return
            old_executor = self._executor
            self._executor = ThreadPoolExecutor(max_workers=new_task_concurrency)
        old_executor.shutdown(wait=False)

    def shutdown(self):
        if self._execution_backend != "thread":
            return
        with self._executor_lock:
            self._executor.shutdown(wait=False)

    def _update_task(self, task_id, **fields):
        self._task_store.update_task(task_id, **fields)

    def _run_download(self, task_id, url, timeout, retries, image_concurrency):
        """Wrapper function to run in a thread and update task status."""
        execute_download_task(
            self._task_store,
            task_id=task_id,
            url=url,
            timeout=timeout,
            retries=retries,
            image_concurrency=image_concurrency,
            logger=self._logger,
        )

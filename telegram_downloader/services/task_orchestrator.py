import threading
from concurrent.futures import ThreadPoolExecutor

from telegram_downloader.services.image_downloader import (
    DownloadCancelledError,
    PartialDownloadError,
    download_images,
)


class TaskOrchestrator:
    """Coordinates task execution and worker pool lifecycle."""

    def __init__(self, task_store, logger, initial_task_concurrency):
        self._task_store = task_store
        self._logger = logger
        self._executor_lock = threading.Lock()
        self._executor = ThreadPoolExecutor(max_workers=initial_task_concurrency)

    def submit_download(self, task_id, url, timeout, retries, image_concurrency):
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
        with self._executor_lock:
            if self._executor._max_workers == new_task_concurrency:
                return
            old_executor = self._executor
            self._executor = ThreadPoolExecutor(max_workers=new_task_concurrency)
        old_executor.shutdown(wait=False)

    def shutdown(self):
        with self._executor_lock:
            self._executor.shutdown(wait=False)

    def _update_task(self, task_id, **fields):
        self._task_store.update_task(task_id, **fields)

    def _run_download(self, task_id, url, timeout, retries, image_concurrency):
        """Wrapper function to run in a thread and update task status."""
        try:
            if not self._task_store.mark_in_progress(task_id):
                current_status = self._task_store.get_field(task_id, "status", "")
                if current_status == "CANCEL_REQUESTED":
                    self._update_task(task_id, status="CANCELED", error="Cancelled before start.")
                    self._logger.info("Task %s - Status: CANCELED before start", task_id)
                return
            self._logger.info("Task %s - Status: IN_PROGRESS", task_id)

            result_zip_path = download_images(
                url,
                timeout=timeout,
                retries=retries,
                task_id=task_id,
                image_concurrency=image_concurrency,
                tasks_db=self._task_store,
            )

            self._logger.info("Task %s - Status: SUCCESS", task_id)
            self._update_task(task_id, status="SUCCESS", result_zip_path=result_zip_path)
        except DownloadCancelledError:
            self._logger.info("Task %s - Status: CANCELED", task_id)
            self._update_task(task_id, status="CANCELED", error="Cancelled by user.", result_zip_path=None)
        except PartialDownloadError as exc:
            self._logger.warning("Task %s - Status: FAILED (partial), Error: %s", task_id, exc)
            self._update_task(task_id, status="FAILED", error=str(exc), result_zip_path=None)
        except Exception as exc:
            self._logger.error("Task %s - Status: FAILED, Error: %s", task_id, exc)
            self._update_task(task_id, status="FAILED", error=str(exc), result_zip_path=None)

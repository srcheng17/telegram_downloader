import logging
import os

from telegram_downloader.celery_app import celery_app
from telegram_downloader.repositories.task_store import TaskStore
from telegram_downloader.services.download_worker import execute_download_task


LOGGER = logging.getLogger(__name__)
_TASK_STORE = None


def _resolve_task_store():
    global _TASK_STORE
    if _TASK_STORE is not None:
        return _TASK_STORE
    project_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    tasks_db_path = os.environ.get("TASKS_DB_PATH", os.path.join(project_root, "data", "tasks.db"))
    _TASK_STORE = TaskStore(tasks_db_path)
    return _TASK_STORE


@celery_app.task(name="telegram_downloader.download_task")
def download_task(task_id, url, timeout, retries, image_concurrency):
    execute_download_task(
        _resolve_task_store(),
        task_id=task_id,
        url=url,
        timeout=timeout,
        retries=retries,
        image_concurrency=image_concurrency,
        logger=LOGGER,
    )
    return {"task_id": task_id, "status": "done"}

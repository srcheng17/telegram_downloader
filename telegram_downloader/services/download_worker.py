import logging

from telegram_downloader.services.image_downloader import (
    DownloadCancelledError,
    PartialDownloadError,
    download_images,
)


LOGGER = logging.getLogger(__name__)


def execute_download_task(task_store, task_id, url, timeout, retries, image_concurrency, logger=None):
    """Run one download job and persist state transitions."""
    active_logger = logger or LOGGER
    try:
        if not task_store.mark_in_progress(task_id):
            current_status = task_store.get_field(task_id, "status", "")
            if current_status == "CANCEL_REQUESTED":
                task_store.update_task(task_id, status="CANCELED", error="Cancelled before start.")
                active_logger.info("Task %s - Status: CANCELED before start", task_id)
            return
        active_logger.info("Task %s - Status: IN_PROGRESS", task_id)

        result_zip_path = download_images(
            url,
            timeout=timeout,
            retries=retries,
            task_id=task_id,
            image_concurrency=image_concurrency,
            tasks_db=task_store,
        )

        active_logger.info("Task %s - Status: SUCCESS", task_id)
        task_store.update_task(task_id, status="SUCCESS", result_zip_path=result_zip_path)
    except DownloadCancelledError:
        active_logger.info("Task %s - Status: CANCELED", task_id)
        task_store.update_task(task_id, status="CANCELED", error="Cancelled by user.", result_zip_path=None)
    except PartialDownloadError as exc:
        active_logger.warning("Task %s - Status: FAILED (partial), Error: %s", task_id, exc)
        task_store.update_task(task_id, status="FAILED", error=str(exc), result_zip_path=None)
    except Exception as exc:
        active_logger.error("Task %s - Status: FAILED, Error: %s", task_id, exc)
        task_store.update_task(task_id, status="FAILED", error=str(exc), result_zip_path=None)

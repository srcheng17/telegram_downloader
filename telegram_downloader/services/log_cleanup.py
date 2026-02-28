import os
import shutil
import threading
import time

from telegram_downloader.constants import ACTIVE_TASK_STATUSES


class LogCleanupService:
    """Background worker that periodically removes expired task logs and files."""

    def __init__(
        self,
        task_store,
        logger,
        get_retention_days,
        get_file_retention_days=None,
        interval_seconds=3600,
    ):
        self._task_store = task_store
        self._logger = logger
        self._get_retention_days = get_retention_days
        self._get_file_retention_days = get_file_retention_days or get_retention_days
        self._interval_seconds = interval_seconds
        self._stop_event = threading.Event()
        self._thread = None

    def start(self):
        if self._thread and self._thread.is_alive():
            return
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def stop(self):
        self._stop_event.set()
        if self._thread and self._thread.is_alive():
            self._thread.join(timeout=1)

    def _normalize_retention_days(self, days, fallback):
        try:
            parsed = int(days)
        except (TypeError, ValueError):
            return fallback
        return max(1, parsed)

    def _safe_common_path(self, candidate, root):
        try:
            return os.path.commonpath([candidate, root]) == root
        except ValueError:
            return False

    def _remove_file_if_within_root(self, file_path, root_path):
        if not file_path:
            return False
        real_root = os.path.realpath(root_path)
        real_candidate = os.path.realpath(file_path)
        if not self._safe_common_path(real_candidate, real_root):
            self._logger.warning("Skipped cleanup for unsafe path: %s", file_path)
            return False
        if not os.path.isfile(real_candidate):
            return False
        try:
            os.remove(real_candidate)
            return True
        except OSError as exc:
            self._logger.warning("Failed to remove file %s: %s", real_candidate, exc)
            return False

    def _cleanup_expired_task_logs_and_files(self, cutoff_start_time, download_root):
        expired_tasks = self._task_store.list_tasks_with_result_zip_older_than(
            cutoff_start_time,
            exclude_statuses=ACTIVE_TASK_STATUSES,
        )
        removed_files = 0
        for task in expired_tasks:
            if self._remove_file_if_within_root(task.get("result_zip_path"), download_root):
                removed_files += 1

        deleted_count = self._task_store.delete_older_than(
            cutoff_start_time,
            exclude_statuses=ACTIVE_TASK_STATUSES,
        )

        if deleted_count:
            self._logger.info(
                "Deleted %s expired task logs (removed %s associated files)",
                deleted_count,
                removed_files,
            )

    def _cleanup_expired_task_files(self, cutoff_start_time, download_root):
        expired_tasks = self._task_store.list_tasks_with_result_zip_older_than(
            cutoff_start_time,
            exclude_statuses=ACTIVE_TASK_STATUSES,
        )

        removed_files = 0
        cleared_paths = 0
        for task in expired_tasks:
            task_id = task.get("id")
            removed = self._remove_file_if_within_root(task.get("result_zip_path"), download_root)
            if removed or task.get("result_zip_path"):
                self._task_store.clear_result_zip_path(task_id)
                cleared_paths += 1
            if removed:
                removed_files += 1

        if removed_files or cleared_paths:
            self._logger.info(
                "Cleaned %s expired task files (cleared %s DB references)",
                removed_files,
                cleared_paths,
            )

    def _cleanup_orphaned_files(self, cutoff_file_time, download_root, temp_root):
        referenced_paths = {
            os.path.realpath(path)
            for path in self._task_store.list_result_zip_paths()
            if path
        }

        removed_orphaned = 0
        if os.path.isdir(download_root):
            for name in os.listdir(download_root):
                candidate = os.path.join(download_root, name)
                real_candidate = os.path.realpath(candidate)
                if not self._safe_common_path(real_candidate, os.path.realpath(download_root)):
                    continue
                if not os.path.isfile(real_candidate):
                    continue
                try:
                    if os.path.getmtime(real_candidate) >= cutoff_file_time:
                        continue
                except OSError:
                    continue
                if real_candidate in referenced_paths:
                    continue
                try:
                    os.remove(real_candidate)
                    removed_orphaned += 1
                except OSError as exc:
                    self._logger.warning("Failed to remove orphaned file %s: %s", real_candidate, exc)

        removed_temp = 0
        if os.path.isdir(temp_root):
            for name in os.listdir(temp_root):
                candidate = os.path.join(temp_root, name)
                real_candidate = os.path.realpath(candidate)
                if not self._safe_common_path(real_candidate, os.path.realpath(temp_root)):
                    continue
                try:
                    if os.path.getmtime(real_candidate) >= cutoff_file_time:
                        continue
                except OSError:
                    continue
                try:
                    if os.path.isdir(real_candidate):
                        shutil.rmtree(real_candidate, ignore_errors=True)
                    else:
                        os.remove(real_candidate)
                    removed_temp += 1
                except OSError as exc:
                    self._logger.warning("Failed to remove temp artifact %s: %s", real_candidate, exc)

        if removed_orphaned or removed_temp:
            self._logger.info(
                "Removed %s orphaned downloads and %s stale temp artifacts",
                removed_orphaned,
                removed_temp,
            )

    def _run_once(self):
        log_retention_days = self._normalize_retention_days(self._get_retention_days(), 7)
        file_retention_days = self._normalize_retention_days(
            self._get_file_retention_days(),
            log_retention_days,
        )

        now = time.time()
        cutoff_log_time = now - (log_retention_days * 86400)
        cutoff_file_time = now - (file_retention_days * 86400)

        download_root = os.path.realpath(os.environ.get("DOWNLOAD_PATH", "downloaded_images"))
        temp_root = os.path.realpath(os.environ.get("TEMP_PATH", "temp_downloads"))

        self._cleanup_expired_task_logs_and_files(cutoff_log_time, download_root)
        self._cleanup_expired_task_files(cutoff_file_time, download_root)
        self._cleanup_orphaned_files(cutoff_file_time, download_root, temp_root)

    def _run(self):
        while not self._stop_event.is_set():
            try:
                self._run_once()
            except Exception as exc:
                self._logger.warning("Cleanup cycle failed: %s", exc)
            if self._stop_event.wait(self._interval_seconds):
                return

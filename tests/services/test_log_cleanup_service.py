import logging
import os
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

from telegram_downloader.services.log_cleanup import LogCleanupService


class _FakeTaskStore:
    def __init__(self, tasks):
        self.tasks = list(tasks)

    def _iter_expired_with_zip(self, cutoff_start_time, exclude_statuses):
        exclusions = set(exclude_statuses or [])
        results = []
        for task in self.tasks:
            if task["start_time"] >= cutoff_start_time:
                continue
            if task["status"] in exclusions:
                continue
            path = task.get("result_zip_path")
            if not path:
                continue
            results.append({"id": task["id"], "result_zip_path": path})
        return results

    def list_tasks_with_result_zip_older_than(self, cutoff_start_time, exclude_statuses=None):
        return self._iter_expired_with_zip(cutoff_start_time, exclude_statuses)

    def delete_older_than(self, cutoff_start_time, exclude_statuses=None):
        exclusions = set(exclude_statuses or [])
        before = len(self.tasks)
        self.tasks = [
            task
            for task in self.tasks
            if not (task["start_time"] < cutoff_start_time and task["status"] not in exclusions)
        ]
        return before - len(self.tasks)

    def clear_result_zip_path(self, task_id):
        for task in self.tasks:
            if task["id"] == task_id:
                task["result_zip_path"] = None
                return

    def list_result_zip_paths(self):
        paths = []
        for task in self.tasks:
            path = task.get("result_zip_path")
            if path:
                paths.append(path)
        return paths


class LogCleanupServiceTests(unittest.TestCase):
    def test_run_once_cleans_expired_logs_and_files(self):
        now = time.time()

        with tempfile.TemporaryDirectory() as root_dir:
            download_root = Path(root_dir) / "downloaded_images"
            temp_root = Path(root_dir) / "temp_downloads"
            download_root.mkdir(parents=True, exist_ok=True)
            temp_root.mkdir(parents=True, exist_ok=True)

            old_log_file = download_root / "old-log.zip"
            old_file_only = download_root / "old-file.zip"
            recent_file = download_root / "recent.zip"
            orphan_file = download_root / "orphan.zip"
            for file_path in (old_log_file, old_file_only, recent_file, orphan_file):
                file_path.write_bytes(b"zip")

            stale_temp = temp_root / "stale"
            stale_temp.mkdir(parents=True, exist_ok=True)
            (stale_temp / "tmp.bin").write_bytes(b"tmp")

            fresh_temp = temp_root / "fresh"
            fresh_temp.mkdir(parents=True, exist_ok=True)
            (fresh_temp / "tmp.bin").write_bytes(b"tmp")

            old_mtime = now - (10 * 86400)
            mid_mtime = now - (4 * 86400)
            recent_mtime = now - (12 * 3600)
            for file_path, mtime in (
                (old_log_file, old_mtime),
                (old_file_only, mid_mtime),
                (recent_file, recent_mtime),
                (orphan_file, old_mtime),
            ):
                os.utime(file_path, (mtime, mtime))

            os.utime(stale_temp, (old_mtime, old_mtime))
            os.utime(fresh_temp, (recent_mtime, recent_mtime))

            store = _FakeTaskStore(
                [
                    {
                        "id": "task-old-log",
                        "start_time": now - (9 * 86400),
                        "status": "SUCCESS",
                        "result_zip_path": str(old_log_file),
                    },
                    {
                        "id": "task-old-file",
                        "start_time": now - (3 * 86400),
                        "status": "SUCCESS",
                        "result_zip_path": str(old_file_only),
                    },
                    {
                        "id": "task-recent",
                        "start_time": now - (2 * 3600),
                        "status": "SUCCESS",
                        "result_zip_path": str(recent_file),
                    },
                ]
            )

            cleanup = LogCleanupService(
                task_store=store,
                logger=logging.getLogger("cleanup-test"),
                get_retention_days=lambda: 7,
                get_file_retention_days=lambda: 2,
                interval_seconds=3600,
            )

            with patch.dict(
                os.environ,
                {"DOWNLOAD_PATH": str(download_root), "TEMP_PATH": str(temp_root)},
                clear=False,
            ):
                cleanup._run_once()

            self.assertFalse(old_log_file.exists())
            self.assertFalse(old_file_only.exists())
            self.assertTrue(recent_file.exists())
            self.assertFalse(orphan_file.exists())
            self.assertFalse(stale_temp.exists())
            self.assertTrue(fresh_temp.exists())

            task_ids = {task["id"] for task in store.tasks}
            self.assertNotIn("task-old-log", task_ids)
            self.assertIn("task-old-file", task_ids)
            self.assertIsNone(next(task for task in store.tasks if task["id"] == "task-old-file")["result_zip_path"])


if __name__ == "__main__":
    unittest.main()

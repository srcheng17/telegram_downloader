import tempfile
import unittest
from pathlib import Path

from telegram_downloader.repositories.task_store import TaskStore


class TaskStoreRepositoryTests(unittest.TestCase):
    def setUp(self):
        self._temp_dir = tempfile.TemporaryDirectory()
        db_path = Path(self._temp_dir.name) / "tasks.db"
        self.store = TaskStore(str(db_path))

    def tearDown(self):
        self._temp_dir.cleanup()

    def _add_task(self, task_id, start_time, status="PENDING"):
        self.store.create_task(
            {
                "id": task_id,
                "url": f"https://telegra.ph/{task_id}",
                "status": status,
                "start_time": float(start_time),
                "error": None,
                "progress": 0,
                "total_images": 0,
                "image_concurrency": 2,
            }
        )

    def test_create_update_and_get_field(self):
        self._add_task("task-1", 100, status="PENDING")
        self.store.update_task("task-1", status="IN_PROGRESS", error="tmp", total_images=10)
        self.store.increment_progress("task-1", step=3)

        self.assertEqual(self.store.get_field("task-1", "status", None), "IN_PROGRESS")
        self.assertEqual(self.store.get_field("task-1", "error", None), "tmp")
        self.assertEqual(self.store.get_field("task-1", "total_images", 0), 10)
        self.assertEqual(self.store.get_field("task-1", "progress", 0), 3)

    def test_list_paginated_sorted_and_clamped(self):
        for i in range(5):
            self._add_task(f"task-{i}", 100 + i, status="SUCCESS")

        rows, total, total_pages, page = self.store.list_paginated(page=99, per_page=2)
        self.assertEqual(total, 5)
        self.assertEqual(total_pages, 3)
        self.assertEqual(page, 3)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["id"], "task-0")

        rows, total, total_pages, page = self.store.list_paginated(page=-5, per_page=999)
        self.assertEqual(page, 1)
        self.assertEqual(total_pages, 1)
        self.assertEqual(total, 5)
        self.assertEqual(len(rows), 5)
        self.assertEqual(rows[0]["id"], "task-4")
        self.assertEqual(rows[-1]["id"], "task-0")

    def test_has_active_tasks_and_retention_cleanup(self):
        self._add_task("old-task", 100, status="SUCCESS")
        self._add_task("active-task", 200, status="IN_PROGRESS")

        self.assertTrue(self.store.has_active_tasks({"PENDING", "IN_PROGRESS"}))
        deleted = self.store.delete_older_than(150)
        self.assertEqual(deleted, 1)
        self.assertEqual(self.store.get_field("old-task", "id", None), None)
        self.assertEqual(self.store.get_field("active-task", "status", None), "IN_PROGRESS")

    def test_retention_cleanup_can_exclude_statuses(self):
        self._add_task("old-active", 100, status="IN_PROGRESS")
        self._add_task("old-finished", 100, status="FAILED")

        deleted = self.store.delete_older_than(150, exclude_statuses={"IN_PROGRESS"})
        self.assertEqual(deleted, 1)
        self.assertEqual(self.store.get_field("old-active", "status", None), "IN_PROGRESS")
        self.assertEqual(self.store.get_field("old-finished", "id", None), None)

    def test_mark_in_progress_only_from_pending(self):
        self._add_task("pending-task", 100, status="PENDING")
        self._add_task("cancel-task", 120, status="CANCEL_REQUESTED")

        self.assertTrue(self.store.mark_in_progress("pending-task"))
        self.assertEqual(self.store.get_field("pending-task", "status", None), "IN_PROGRESS")

        self.assertFalse(self.store.mark_in_progress("cancel-task"))
        self.assertEqual(self.store.get_field("cancel-task", "status", None), "CANCEL_REQUESTED")

    def test_result_zip_path_can_be_persisted(self):
        self.store.create_task(
            {
                "id": "success-task",
                "url": "https://telegra.ph/success-task",
                "status": "SUCCESS",
                "start_time": 100,
                "error": None,
                "progress": 10,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/success.zip",
            }
        )
        self.assertEqual(
            self.store.get_field("success-task", "result_zip_path", None),
            "/tmp/success.zip",
        )

    def test_result_zip_helpers_support_cleanup_flow(self):
        self.store.create_task(
            {
                "id": "task-a",
                "url": "https://telegra.ph/task-a",
                "status": "SUCCESS",
                "start_time": 100,
                "error": None,
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/a.zip",
            }
        )
        self.store.create_task(
            {
                "id": "task-b",
                "url": "https://telegra.ph/task-b",
                "status": "IN_PROGRESS",
                "start_time": 100,
                "error": None,
                "progress": 0,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/b.zip",
            }
        )

        rows = self.store.list_tasks_with_result_zip_older_than(150, exclude_statuses={"IN_PROGRESS"})
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["id"], "task-a")

        self.assertIn("/tmp/a.zip", self.store.list_result_zip_paths())
        self.assertIn("/tmp/b.zip", self.store.list_result_zip_paths())

        self.store.clear_result_zip_path("task-a")
        self.assertEqual(self.store.get_field("task-a", "result_zip_path", None), None)


if __name__ == "__main__":
    unittest.main()

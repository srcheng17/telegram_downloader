import tempfile
import threading
import unittest
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from telegram_downloader.repositories.task_store import TaskStore


class TaskStoreRepositoryTests(unittest.TestCase):
    def setUp(self):
        self._temp_dir = tempfile.TemporaryDirectory()
        self._db_path = Path(self._temp_dir.name) / "tasks.db"
        self.store = TaskStore(str(self._db_path))

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

    def test_list_paginated_supports_status_and_keyword_filter(self):
        self.store.create_task(
            {
                "id": "success-keep",
                "url": "https://telegra.ph/success-keep",
                "canonical_url": "https://telegra.ph/success-keep",
                "status": "SUCCESS",
                "start_time": 300,
                "error": "none",
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )
        self.store.create_task(
            {
                "id": "failed-match",
                "url": "https://telegra.ph/failed-match",
                "canonical_url": "https://telegra.ph/failed-match",
                "status": "FAILED",
                "start_time": 200,
                "error": "boom match token",
                "progress": 0,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )
        self.store.create_task(
            {
                "id": "failed-ignore",
                "url": "https://telegra.ph/failed-ignore",
                "canonical_url": "https://telegra.ph/failed-ignore",
                "status": "FAILED",
                "start_time": 100,
                "error": "boom",
                "progress": 0,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )

        rows, total, total_pages, page = self.store.list_paginated(
            page=1,
            per_page=20,
            status="FAILED",
            keyword="match",
        )
        self.assertEqual(total, 1)
        self.assertEqual(total_pages, 1)
        self.assertEqual(page, 1)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["id"], "failed-match")

    def test_get_status_counts_returns_aggregated_values(self):
        self._add_task("pending-1", 300, status="PENDING")
        self._add_task("pending-2", 250, status="PENDING")
        self._add_task("success-1", 200, status="SUCCESS")
        self._add_task("failed-1", 150, status="FAILED")

        counts = self.store.get_status_counts()
        self.assertEqual(counts["PENDING"], 2)
        self.assertEqual(counts["SUCCESS"], 1)
        self.assertEqual(counts["FAILED"], 1)

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

    def test_create_and_get_task_persists_cbz_metadata_fields(self):
        self.store.create_task(
            {
                "id": "cbz-task",
                "url": "https://telegra.ph/cbz-task",
                "canonical_url": "https://telegra.ph/cbz-task",
                "status": "SUCCESS",
                "start_time": 123.0,
                "error": None,
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/cbz-task.zip",
                "author": "author-name",
                "series_name": "series-name",
                "comic_name": "comic-name",
                "summary": "summary text",
                "tags_raw": "tagA, tagB",
                "tags_normalized": "taga,tagb",
                "genres_raw": "genreA, genreB",
                "genres_normalized": "genrea,genreb",
            }
        )

        task = self.store.get_task("cbz-task")
        self.assertEqual(task["author"], "author-name")
        self.assertEqual(task["series_name"], "series-name")
        self.assertEqual(task["comic_name"], "comic-name")
        self.assertEqual(task["summary"], "summary text")
        self.assertEqual(task["tags_raw"], "tagA, tagB")
        self.assertEqual(task["tags_normalized"], "taga,tagb")
        self.assertEqual(task["genres_raw"], "genreA, genreB")
        self.assertEqual(task["genres_normalized"], "genrea,genreb")

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

    def test_find_latest_success_by_canonical_url_prefers_newest_success_with_file(self):
        canonical_url = "https://telegra.ph/dedupe-article"
        self.store.create_task(
            {
                "id": "old-success",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "SUCCESS",
                "start_time": 100,
                "error": None,
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/old.zip",
            }
        )
        self.store.create_task(
            {
                "id": "new-failed",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "FAILED",
                "start_time": 150,
                "error": "nope",
                "progress": 0,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/failed.zip",
            }
        )
        self.store.create_task(
            {
                "id": "new-success-no-file",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "SUCCESS",
                "start_time": 200,
                "error": None,
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )
        self.store.create_task(
            {
                "id": "new-success",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "SUCCESS",
                "start_time": 250,
                "error": None,
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/new.zip",
            }
        )

        hit = self.store.find_latest_success_by_canonical_url(canonical_url)
        self.assertIsNotNone(hit)
        self.assertEqual(hit["id"], "new-success")
        self.assertEqual(hit["result_zip_path"], "/tmp/new.zip")

    def test_find_latest_success_by_canonical_url_normalizes_whitespace(self):
        canonical_url = "https://telegra.ph/strip-me"
        self.store.create_task(
            {
                "id": "strip-success",
                "url": canonical_url,
                "canonical_url": f"  {canonical_url}  ",
                "status": "SUCCESS",
                "start_time": 100,
                "error": None,
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/strip.zip",
            }
        )

        hit = self.store.find_latest_success_by_canonical_url(canonical_url)
        self.assertIsNotNone(hit)
        self.assertEqual(hit["id"], "strip-success")
        self.assertEqual(hit["canonical_url"], canonical_url)

    def test_find_latest_success_by_canonical_url_supports_legacy_rows_without_canonical_url(self):
        canonical_url = "https://telegra.ph/legacy-row"
        self.store.create_task(
            {
                "id": "legacy-success",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "SUCCESS",
                "start_time": 100,
                "error": None,
                "progress": 1,
                "total_images": 1,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/legacy.zip",
            }
        )
        self.store.update_task("legacy-success", canonical_url=None)

        hit = self.store.find_latest_success_by_canonical_url(canonical_url)
        self.assertIsNotNone(hit)
        self.assertEqual(hit["id"], "legacy-success")

    def test_find_latest_active_by_canonical_url_returns_newest_active_task(self):
        canonical_url = "https://telegra.ph/active-dedupe"
        self.store.create_task(
            {
                "id": "pending-task",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "PENDING",
                "start_time": 100,
                "error": None,
                "progress": 0,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )
        self.store.create_task(
            {
                "id": "active-task",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "IN_PROGRESS",
                "start_time": 150,
                "error": None,
                "progress": 2,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )
        self.store.create_task(
            {
                "id": "done-task",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "SUCCESS",
                "start_time": 200,
                "error": None,
                "progress": 10,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/done.zip",
            }
        )

        hit = self.store.find_latest_active_by_canonical_url(
            canonical_url,
            {"PENDING", "IN_PROGRESS", "CANCEL_REQUESTED"},
        )
        self.assertIsNotNone(hit)
        self.assertEqual(hit["id"], "active-task")

    def test_claim_download_task_is_atomic_under_race(self):
        canonical_url = "https://telegra.ph/atomic-race-01-01"
        active_statuses = {"PENDING", "IN_PROGRESS", "CANCEL_REQUESTED"}
        barrier = threading.Barrier(8)
        stores = [self.store, TaskStore(str(self._db_path))]

        def worker(idx):
            barrier.wait()
            return stores[idx % len(stores)].claim_download_task(
                {
                    "id": f"race-task-{idx}",
                    "url": canonical_url,
                    "canonical_url": canonical_url,
                    "status": "PENDING",
                    "start_time": float(100 + idx),
                    "error": None,
                    "progress": 0,
                    "total_images": 0,
                    "image_concurrency": 2,
                    "result_zip_path": None,
                },
                active_statuses,
            )

        with ThreadPoolExecutor(max_workers=8) as executor:
            results = list(executor.map(worker, range(8)))

        created = [result for result in results if result["decision"] == "created"]
        reused_active = [result for result in results if result["decision"] == "reuse_active"]
        self.assertEqual(len(created), 1)
        self.assertEqual(len(reused_active), 7)

        created_task_id = created[0]["task"]["id"]
        self.assertTrue(all(result["task"]["id"] == created_task_id for result in results))

        rows, total, _, _ = self.store.list_paginated(page=1, per_page=50)
        self.assertEqual(total, 1)
        self.assertEqual(rows[0]["id"], created_task_id)
        self.assertEqual(rows[0]["status"], "PENDING")
        self.assertEqual(rows[0]["canonical_url"], canonical_url)


if __name__ == "__main__":
    unittest.main()

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from app import app, app_settings, runtime, task_store
from telegram_downloader.constants import TASK_RECOVERY_FAILED_REASON


class DownloadSubmissionIntegrationTests(unittest.TestCase):
    def setUp(self):
        app.testing = True
        self.client = app.test_client()
        self.original_settings = dict(app_settings)
        task_store.clear()

    def tearDown(self):
        task_store.clear()
        app_settings.clear()
        app_settings.update(self.original_settings)

    def test_download_rejects_non_telegraph_url(self):
        response = self.client.post("/download", data={"url": "https://example.com/not-allowed"})
        self.assertEqual(response.status_code, 302)
        self.assertTrue(response.headers["Location"].endswith("/"))

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 0)
        self.assertEqual(rows, [])

    def test_download_creates_pending_task_and_submits_job(self):
        url = "https://telegra.ph/Test-Article-01-01"
        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post("/download", data={"url": url})

        self.assertEqual(response.status_code, 303)
        self.assertTrue(response.headers["Location"].endswith("/"))
        submit_mock.assert_called_once()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 1)
        task = rows[0]
        self.assertEqual(task["url"], url)
        self.assertEqual(task["status"], "PENDING")
        self.assertEqual(task["image_concurrency"], app_settings["image_concurrency"])

        call_args = submit_mock.call_args[0]
        self.assertEqual(call_args[1], url)
        self.assertEqual(call_args[2], app_settings["timeout"])
        self.assertEqual(call_args[3], app_settings["retries"])
        self.assertEqual(call_args[4], app_settings["image_concurrency"])

    def test_download_uses_runtime_settings_when_submitting(self):
        runtime.update_settings(
            {
                "task_concurrency": 2,
                "image_concurrency": 5,
                "timeout": 40,
                "retries": 11,
                "log_retention_days": 7,
                "file_retention_days": 7,
            }
        )
        with self.client.session_transaction() as flask_session:
            flask_session["settings"] = {
                "task_concurrency": 2,
                "image_concurrency": 7,
                "timeout": 99,
                "retries": 2,
                "log_retention_days": 7,
                "file_retention_days": 7,
            }

        url = "https://telegra.ph/Test-Session-Settings-01-01"
        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post("/download", data={"url": url})

        self.assertEqual(response.status_code, 303)
        submit_mock.assert_called_once()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 1)
        self.assertEqual(rows[0]["image_concurrency"], 5)

        call_args = submit_mock.call_args[0]
        self.assertEqual(call_args[2], 40)
        self.assertEqual(call_args[3], 11)
        self.assertEqual(call_args[4], 5)

    def test_download_reuses_existing_success_zip_when_duplicate(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            download_root = Path(temp_dir) / "downloaded_images"
            download_root.mkdir(parents=True, exist_ok=True)
            zip_path = download_root / "existing.zip"
            zip_path.write_bytes(b"PK\x03\x04existing")

            existing_url = "https://telegra.ph/Test-Article-01-01?utm=abc#frag"
            task_store.create_task(
                {
                    "id": "done-task",
                    "url": existing_url,
                    "canonical_url": "https://telegra.ph/Test-Article-01-01",
                    "status": "SUCCESS",
                    "start_time": 100,
                    "error": None,
                    "progress": 8,
                    "total_images": 8,
                    "image_concurrency": 2,
                    "result_zip_path": str(zip_path),
                }
            )

            with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
                with patch.dict(os.environ, {"DOWNLOAD_PATH": str(download_root)}):
                    response = self.client.post("/download", data={"url": "https://www.telegra.ph/Test-Article-01-01"})

        self.assertEqual(response.status_code, 303)
        self.assertTrue(response.headers["Location"].endswith("/"))
        submit_mock.assert_not_called()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 1)
        self.assertEqual(rows[0]["id"], "done-task")

    def test_download_queues_new_task_when_duplicate_file_is_missing(self):
        task_store.create_task(
            {
                "id": "done-missing",
                "url": "https://telegra.ph/Test-Missing-01-01",
                "canonical_url": "https://telegra.ph/Test-Missing-01-01",
                "status": "SUCCESS",
                "start_time": 100,
                "error": None,
                "progress": 8,
                "total_images": 8,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/not-found.zip",
            }
        )

        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post("/download", data={"url": "https://telegra.ph/Test-Missing-01-01"})

        self.assertEqual(response.status_code, 303)
        self.assertTrue(response.headers["Location"].endswith("/"))
        submit_mock.assert_called_once()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 2)

    def test_download_force_true_creates_new_task_instead_of_reusing_success(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            download_root = Path(temp_dir) / "downloaded_images"
            download_root.mkdir(parents=True, exist_ok=True)
            zip_path = download_root / "existing.zip"
            zip_path.write_bytes(b"PK\x03\x04existing")
            canonical_url = "https://telegra.ph/Test-Force-01-01"

            task_store.create_task(
                {
                    "id": "done-force-task",
                    "url": canonical_url,
                    "canonical_url": canonical_url,
                    "status": "SUCCESS",
                    "start_time": 100,
                    "error": None,
                    "progress": 8,
                    "total_images": 8,
                    "image_concurrency": 2,
                    "result_zip_path": str(zip_path),
                }
            )

            with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
                with patch.dict(os.environ, {"DOWNLOAD_PATH": str(download_root)}):
                    response = self.client.post(
                        "/download",
                        json={
                            "url": "https://www.telegra.ph/Test-Force-01-01",
                            "force": True,
                        },
                    )

        self.assertEqual(response.status_code, 202)
        payload = response.get_json()
        self.assertTrue(payload["ok"])
        self.assertFalse(payload["duplicate"])
        self.assertFalse(payload["active"])
        self.assertTrue(payload["force_applied"])
        self.assertNotEqual(payload["task_id"], "done-force-task")
        submit_mock.assert_called_once()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 2)
        self.assertEqual(rows[0]["id"], payload["task_id"])
        self.assertEqual(rows[0]["status"], "PENDING")

    def test_download_force_confirmation_creates_new_task_with_metadata(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            download_root = Path(temp_dir) / "downloaded_images"
            download_root.mkdir(parents=True, exist_ok=True)
            zip_path = download_root / "existing.zip"
            zip_path.write_bytes(b"PK\x03\x04existing")
            canonical_url = "https://telegra.ph/Test-Metadata-Force-01-01"

            task_store.create_task(
                {
                    "id": "done-metadata-task",
                    "url": canonical_url,
                    "canonical_url": canonical_url,
                    "status": "SUCCESS",
                    "start_time": 100,
                    "error": None,
                    "progress": 8,
                    "total_images": 8,
                    "image_concurrency": 2,
                    "result_zip_path": str(zip_path),
                }
            )

            metadata_payload = {
                "author": "  Author Name  ",
                "series_name": "  Series Name ",
                "comic_name": "  Comic Name ",
                "summary": "  Summary line ",
                "tags": "  tagA， tagB,tagC  ",
                "genres": "  genreA， genreB,genreC  ",
            }
            with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
                with patch.dict(os.environ, {"DOWNLOAD_PATH": str(download_root)}):
                    confirm_response = self.client.post(
                        "/download",
                        json={
                            "url": "https://www.telegra.ph/Test-Metadata-Force-01-01",
                            **metadata_payload,
                        },
                    )
                    force_response = self.client.post(
                        "/download",
                        json={
                            "url": "https://www.telegra.ph/Test-Metadata-Force-01-01",
                            "force": True,
                            **metadata_payload,
                        },
                    )

        self.assertEqual(confirm_response.status_code, 200)
        confirm_payload = confirm_response.get_json()
        self.assertTrue(confirm_payload["duplicate"])
        self.assertTrue(confirm_payload["needs_confirmation"])
        self.assertEqual(confirm_payload["task_id"], "done-metadata-task")
        self.assertIn("/api/tasks/done-metadata-task/download", confirm_payload["download_url"])

        self.assertEqual(force_response.status_code, 202)
        force_payload = force_response.get_json()
        self.assertTrue(force_payload["ok"])
        self.assertFalse(force_payload["duplicate"])
        self.assertFalse(force_payload["active"])
        self.assertTrue(force_payload["force_applied"])
        self.assertNotEqual(force_payload["task_id"], "done-metadata-task")
        submit_mock.assert_called_once()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 2)
        self.assertEqual(rows[0]["id"], force_payload["task_id"])
        self.assertEqual(rows[0]["author"], "Author Name")
        self.assertEqual(rows[0]["series_name"], "Series Name")
        self.assertEqual(rows[0]["comic_name"], "Comic Name")
        self.assertEqual(rows[0]["summary"], "Summary line")
        self.assertEqual(rows[0]["tags_raw"], "tagA， tagB,tagC")
        self.assertEqual(rows[0]["tags_normalized"], "tagA, tagB,tagC")
        self.assertEqual(rows[0]["genres_raw"], "genreA， genreB,genreC")
        self.assertEqual(rows[0]["genres_normalized"], "genreA, genreB,genreC")

    def test_download_force_true_still_reuses_existing_active_task(self):
        task_store.create_task(
            {
                "id": "active-force-task",
                "url": "https://telegra.ph/Test-Active-Force-01-01",
                "canonical_url": "https://telegra.ph/Test-Active-Force-01-01",
                "status": "IN_PROGRESS",
                "start_time": 100,
                "error": None,
                "progress": 2,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )

        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post(
                "/download",
                data={
                    "url": "https://www.telegra.ph/Test-Active-Force-01-01",
                    "force": "true",
                },
            )

        self.assertEqual(response.status_code, 303)
        self.assertTrue(response.headers["Location"].endswith("/"))
        submit_mock.assert_not_called()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 1)
        self.assertEqual(rows[0]["id"], "active-force-task")

    def test_download_reuses_existing_active_task_and_skips_new_submission(self):
        task_store.create_task(
            {
                "id": "active-task",
                "url": "https://telegra.ph/Test-Active-01-01",
                "canonical_url": "https://telegra.ph/Test-Active-01-01",
                "status": "IN_PROGRESS",
                "start_time": 100,
                "error": None,
                "progress": 2,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )

        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post("/download", data={"url": "https://www.telegra.ph/Test-Active-01-01"})

        self.assertEqual(response.status_code, 303)
        self.assertTrue(response.headers["Location"].endswith("/"))
        submit_mock.assert_not_called()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 1)
        self.assertEqual(rows[0]["id"], "active-task")

    def test_download_allows_resubmit_after_startup_recovery_marks_active_task(self):
        canonical_url = "https://telegra.ph/Test-Recovery-01-01"
        task_store.create_task(
            {
                "id": "stale-active-task",
                "url": canonical_url,
                "canonical_url": canonical_url,
                "status": "IN_PROGRESS",
                "start_time": 100,
                "error": None,
                "progress": 3,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": "/tmp/stale.zip",
            }
        )

        runtime.recovery_service.recover_stale_active_tasks(now=100)
        stale_task = task_store.get_task("stale-active-task")
        self.assertEqual(stale_task["status"], "FAILED")
        self.assertEqual(stale_task["error"], TASK_RECOVERY_FAILED_REASON)
        self.assertIsNone(stale_task["result_zip_path"])

        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post("/download", data={"url": "https://www.telegra.ph/Test-Recovery-01-01"})

        self.assertEqual(response.status_code, 303)
        self.assertTrue(response.headers["Location"].endswith("/"))
        submit_mock.assert_called_once()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 2)
        self.assertEqual(rows[0]["status"], "PENDING")
        self.assertNotEqual(rows[0]["id"], "stale-active-task")

    def test_download_json_returns_duplicate_active_task_payload(self):
        task_store.create_task(
            {
                "id": "active-json-task",
                "url": "https://telegra.ph/Test-Json-Active-01-01",
                "canonical_url": "https://telegra.ph/Test-Json-Active-01-01",
                "status": "IN_PROGRESS",
                "start_time": 100,
                "error": None,
                "progress": 3,
                "total_images": 10,
                "image_concurrency": 2,
                "result_zip_path": None,
            }
        )

        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post(
                "/download",
                json={"url": "https://www.telegra.ph/Test-Json-Active-01-01"},
            )

        self.assertEqual(response.status_code, 200)
        payload = response.get_json()
        self.assertTrue(payload["ok"])
        self.assertTrue(payload["duplicate"])
        self.assertTrue(payload["active"])
        self.assertEqual(payload["task_id"], "active-json-task")
        submit_mock.assert_not_called()


if __name__ == "__main__":
    unittest.main()

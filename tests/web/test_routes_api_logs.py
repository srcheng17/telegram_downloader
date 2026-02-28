import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from app import app, app_settings, normalized_settings, runtime, task_store


class WebRoutesApiLogsTests(unittest.TestCase):
    def setUp(self):
        app.testing = True
        self.client = app.test_client()
        self.original_settings = dict(app_settings)
        task_store.clear()

    def tearDown(self):
        task_store.clear()
        app_settings.clear()
        app_settings.update(self.original_settings)

    def _add_task(self, task_id, start_time, status, result_zip_path=None):
        task_store.create_task(
            {
                "id": task_id,
                "url": f"https://telegra.ph/{task_id}",
                "status": status,
                "start_time": start_time,
                "error": None,
                "progress": 0,
                "total_images": 0,
                "image_concurrency": 2,
                "result_zip_path": result_zip_path,
            }
        )

    def test_api_logs_clamps_page_and_per_page(self):
        for idx in range(5):
            self._add_task(task_id=f"task-{idx}", start_time=100 + idx, status="SUCCESS")

        response = self.client.get("/api/logs?page=99&per_page=2")
        self.assertEqual(response.status_code, 200)
        payload = response.get_json()

        self.assertEqual(payload["per_page"], 2)
        self.assertEqual(payload["total"], 5)
        self.assertEqual(payload["total_pages"], 3)
        self.assertEqual(payload["page"], 3)
        self.assertEqual(len(payload["logs"]), 1)
        self.assertEqual(payload["logs"][0]["id"], "task-0")
        self.assertFalse(payload["has_active_tasks"])

        response = self.client.get("/api/logs?page=-10&per_page=999")
        self.assertEqual(response.status_code, 200)
        payload = response.get_json()

        self.assertEqual(payload["page"], 1)
        self.assertEqual(payload["per_page"], 100)
        self.assertEqual(payload["total_pages"], 1)
        self.assertEqual(len(payload["logs"]), 5)

    def test_api_logs_marks_active_tasks(self):
        self._add_task(task_id="pending-1", start_time=200, status="PENDING")
        self._add_task(task_id="done-1", start_time=100, status="SUCCESS")

        response = self.client.get("/api/logs?page=1&per_page=10")
        self.assertEqual(response.status_code, 200)
        payload = response.get_json()

        self.assertTrue(payload["has_active_tasks"])

    def test_normalized_settings_supports_legacy_concurrency_key(self):
        settings = normalized_settings({"concurrency": "3"}, app_settings)
        self.assertEqual(settings["task_concurrency"], 3)
        self.assertEqual(settings["image_concurrency"], 3)

    def test_cancel_task_endpoint_updates_status(self):
        self._add_task(task_id="to-cancel", start_time=100, status="PENDING")

        response = self.client.post("/api/tasks/to-cancel/cancel")
        self.assertEqual(response.status_code, 202)
        payload = response.get_json()
        self.assertTrue(payload["ok"])

        self.assertEqual(task_store.get_field("to-cancel", "status", None), "CANCEL_REQUESTED")

    def test_cancel_task_endpoint_handles_missing_or_finished(self):
        response = self.client.post("/api/tasks/not-exist/cancel")
        self.assertEqual(response.status_code, 404)

        self._add_task(task_id="done-task", start_time=100, status="SUCCESS")
        response = self.client.post("/api/tasks/done-task/cancel")
        self.assertEqual(response.status_code, 409)

    def test_settings_post_updates_runtime_and_session(self):
        new_settings = {
            "task_concurrency": "3",
            "image_concurrency": "4",
            "timeout": "45",
            "retries": "6",
            "log_retention_days": "8",
            "file_retention_days": "9",
        }
        with patch.object(runtime.task_orchestrator, "update_task_concurrency") as update_mock:
            response = self.client.post("/settings", data=new_settings)

        self.assertEqual(response.status_code, 302)
        self.assertTrue(response.headers["Location"].endswith("/settings"))
        update_mock.assert_called_once_with(3)

        self.assertEqual(app_settings["task_concurrency"], 3)
        self.assertEqual(app_settings["image_concurrency"], 4)
        self.assertEqual(app_settings["timeout"], 45)
        self.assertEqual(app_settings["retries"], 6)
        self.assertEqual(app_settings["log_retention_days"], 8)
        self.assertEqual(app_settings["file_retention_days"], 9)

        with self.client.session_transaction() as flask_session:
            self.assertEqual(flask_session["settings"]["task_concurrency"], 3)
            self.assertEqual(flask_session["settings"]["image_concurrency"], 4)
            self.assertEqual(flask_session["settings"]["file_retention_days"], 9)

    def test_download_task_file_endpoint_returns_zip(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            zip_path = Path(temp_dir) / "sample.zip"
            zip_path.write_bytes(b"PK\x03\x04fakezip")
            self._add_task(
                task_id="done-with-file",
                start_time=100,
                status="SUCCESS",
                result_zip_path=str(zip_path),
            )

            with patch.dict(os.environ, {"DOWNLOAD_PATH": temp_dir}):
                response = self.client.get("/api/tasks/done-with-file/download")

        self.assertEqual(response.status_code, 200)
        self.assertIn("attachment", response.headers.get("Content-Disposition", ""))
        response.close()

    def test_download_task_file_endpoint_rejects_not_completed(self):
        self._add_task(task_id="running-task", start_time=100, status="IN_PROGRESS")
        response = self.client.get("/api/tasks/running-task/download")
        self.assertEqual(response.status_code, 409)

    def test_download_task_file_endpoint_handles_missing_file(self):
        self._add_task(
            task_id="done-missing",
            start_time=100,
            status="SUCCESS",
            result_zip_path="/tmp/not-exist.zip",
        )
        response = self.client.get("/api/tasks/done-missing/download")
        self.assertEqual(response.status_code, 404)


if __name__ == "__main__":
    unittest.main()

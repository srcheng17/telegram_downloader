import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from app import app, app_settings, normalized_settings, runtime, task_store
from telegram_downloader.constants import DOWNLOAD_GUARDRAILS, STATUS_CATALOG


class WebRoutesApiLogsTests(unittest.TestCase):
    def setUp(self):
        app.testing = True
        self.client = app.test_client()
        self.original_settings = dict(app_settings)
        self.original_startup_recovery = runtime.get_startup_recovery()
        runtime.set_startup_recovery(None)
        task_store.clear()

    def tearDown(self):
        task_store.clear()
        app_settings.clear()
        app_settings.update(self.original_settings)
        runtime.set_startup_recovery(self.original_startup_recovery)

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

    def test_api_logs_includes_status_catalog_metadata(self):
        response = self.client.get("/api/logs?page=1&per_page=10")
        self.assertEqual(response.status_code, 200)
        payload = response.get_json()

        self.assertIn("status_catalog", payload)
        self.assertEqual(payload["status_catalog"], STATUS_CATALOG)

    def test_api_logs_supports_status_and_keyword_filters(self):
        self._add_task(task_id="match-success", start_time=300, status="SUCCESS")
        self._add_task(task_id="match-failed", start_time=200, status="FAILED")
        self._add_task(task_id="ignore-pending", start_time=100, status="PENDING")

        response = self.client.get("/api/logs?page=1&per_page=10&status=FAILED&q=match")
        self.assertEqual(response.status_code, 200)
        payload = response.get_json()

        self.assertEqual(payload["total"], 1)
        self.assertEqual(len(payload["logs"]), 1)
        self.assertEqual(payload["logs"][0]["id"], "match-failed")
        self.assertEqual(payload["filters"]["status"], "FAILED")
        self.assertEqual(payload["filters"]["q"], "match")

    def test_api_summary_returns_aggregated_metrics(self):
        self._add_task(task_id="pending-1", start_time=500, status="PENDING")
        self._add_task(task_id="progress-1", start_time=400, status="IN_PROGRESS")
        self._add_task(task_id="done-1", start_time=300, status="SUCCESS")
        self._add_task(task_id="done-2", start_time=200, status="FAILED")
        self._add_task(task_id="done-3", start_time=100, status="CANCELED")

        response = self.client.get("/api/summary")
        self.assertEqual(response.status_code, 200)
        payload = response.get_json()

        self.assertEqual(payload["total_tasks"], 5)
        self.assertEqual(payload["active_tasks"], 2)
        self.assertEqual(payload["finished_tasks"], 3)
        self.assertEqual(payload["success_tasks"], 1)
        self.assertEqual(payload["failed_tasks"], 1)
        self.assertEqual(payload["canceled_tasks"], 1)
        self.assertEqual(payload["success_rate"], 33.3)
        self.assertIn("startup_recovery", payload)
        self.assertFalse(payload["startup_recovery"]["happened"])
        self.assertEqual(payload["startup_recovery"]["recovered_total"], 0)

    def test_api_summary_includes_startup_recovery_snapshot(self):
        runtime.set_startup_recovery(
            {
                "happened": True,
                "recovered_total": 3,
                "recovered_failed": 2,
                "recovered_canceled": 1,
                "occurred_at": 123.0,
            }
        )

        response = self.client.get("/api/summary")
        self.assertEqual(response.status_code, 200)
        payload = response.get_json()

        self.assertEqual(
            payload["startup_recovery"],
            {
                "happened": True,
                "recovered_total": 3,
                "recovered_failed": 2,
                "recovered_canceled": 1,
                "occurred_at": 123.0,
            },
        )

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

    def test_settings_post_updates_runtime_only(self):
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
            self.assertNotIn("settings", flask_session)

    def test_download_task_file_endpoint_returns_cbz_with_comic_mimetype(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            zip_path = Path(temp_dir) / "sample.cbz"
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
        self.assertIn("sample.cbz", response.headers.get("Content-Disposition", ""))
        self.assertEqual(response.mimetype, "application/vnd.comicbook+zip")
        response.close()

    def test_download_task_file_endpoint_supports_head_precheck(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            zip_path = Path(temp_dir) / "head-check.zip"
            zip_path.write_bytes(b"PK\x03\x04fakezip")
            self._add_task(
                task_id="done-head-check",
                start_time=100,
                status="SUCCESS",
                result_zip_path=str(zip_path),
            )

            with patch.dict(os.environ, {"DOWNLOAD_PATH": temp_dir}):
                response = self.client.head("/api/tasks/done-head-check/download")

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.get_data(), b"")

    def test_download_task_file_endpoint_rejects_not_completed(self):
        self._add_task(task_id="running-task", start_time=100, status="IN_PROGRESS")
        response = self.client.get("/api/tasks/running-task/download")
        self.assertEqual(response.status_code, 409)

    def test_download_supports_json_submission(self):
        url = "https://telegra.ph/Json-Route-01-01"
        with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
            response = self.client.post("/download", json={"url": url})

        self.assertEqual(response.status_code, 202)
        payload = response.get_json()
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["duplicate"], False)
        self.assertFalse(payload["active"])
        self.assertIn("logs_url", payload)
        submit_mock.assert_called_once()

    def test_download_json_duplicate_success_requires_confirmation(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            download_root = Path(temp_dir) / "downloaded_images"
            download_root.mkdir(parents=True, exist_ok=True)
            zip_path = download_root / "done.zip"
            zip_path.write_bytes(b"PK\x03\x04existing")
            self._add_task(
                task_id="done-json-task",
                start_time=100,
                status="SUCCESS",
                result_zip_path=str(zip_path),
            )

            with patch.object(runtime.task_orchestrator, "submit_download") as submit_mock:
                with patch.dict(os.environ, {"DOWNLOAD_PATH": str(download_root)}):
                    response = self.client.post(
                        "/download",
                        json={"url": "https://telegra.ph/done-json-task"},
                    )

        self.assertEqual(response.status_code, 200)
        payload = response.get_json()
        self.assertTrue(payload["ok"])
        self.assertTrue(payload["duplicate"])
        self.assertTrue(payload["needs_confirmation"])
        self.assertEqual(payload["task_id"], "done-json-task")
        self.assertIn("/api/tasks/done-json-task/download", payload["download_url"])
        submit_mock.assert_not_called()

    def test_download_task_file_endpoint_handles_missing_file(self):
        self._add_task(
            task_id="done-missing",
            start_time=100,
            status="SUCCESS",
            result_zip_path="/tmp/not-exist.zip",
        )
        response = self.client.get("/api/tasks/done-missing/download")
        self.assertEqual(response.status_code, 404)

    def test_index_route_injects_download_guardrails_context(self):
        with patch("telegram_downloader.web.routes.render_template", return_value="ok") as render_mock:
            response = self.client.get("/")

        self.assertEqual(response.status_code, 200)
        render_mock.assert_called_once()
        _, kwargs = render_mock.call_args
        self.assertIn("download_guardrails", kwargs)
        self.assertEqual(kwargs["download_guardrails"], dict(DOWNLOAD_GUARDRAILS))


if __name__ == "__main__":
    unittest.main()

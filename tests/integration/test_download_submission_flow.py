import unittest
from unittest.mock import patch

from app import app, app_settings, runtime, task_store


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

        self.assertEqual(response.status_code, 302)
        self.assertTrue(response.headers["Location"].endswith("/logs"))
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

    def test_download_uses_session_settings_when_submitting(self):
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

        self.assertEqual(response.status_code, 302)
        submit_mock.assert_called_once()

        rows, total, _, _ = task_store.list_paginated(page=1, per_page=20)
        self.assertEqual(total, 1)
        self.assertEqual(rows[0]["image_concurrency"], 7)

        call_args = submit_mock.call_args[0]
        self.assertEqual(call_args[2], 99)
        self.assertEqual(call_args[3], 2)
        self.assertEqual(call_args[4], 7)


if __name__ == "__main__":
    unittest.main()

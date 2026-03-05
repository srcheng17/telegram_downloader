import unittest
from unittest.mock import Mock

from telegram_downloader.services.task_orchestrator import TaskOrchestrator


class _FakeExecutor:
    def __init__(self):
        self.calls = []
        self._max_workers = 2

    def submit(self, fn, *args):
        self.calls.append((fn, args))

    def shutdown(self, wait=False):
        del wait


class TaskOrchestratorServiceTests(unittest.TestCase):
    def test_submit_download_uses_celery_backend_when_configured(self):
        celery_app = Mock()
        orchestrator = TaskOrchestrator(
            task_store=Mock(),
            logger=Mock(),
            initial_task_concurrency=2,
            execution_backend="celery",
            celery_app=celery_app,
        )

        orchestrator.submit_download(
            "task-id",
            "https://telegra.ph/demo",
            timeout=30,
            retries=5,
            image_concurrency=3,
        )

        celery_app.send_task.assert_called_once_with(
            "telegram_downloader.download_task",
            kwargs={
                "task_id": "task-id",
                "url": "https://telegra.ph/demo",
                "timeout": 30,
                "retries": 5,
                "image_concurrency": 3,
            },
        )

    def test_submit_download_uses_thread_executor_by_default(self):
        orchestrator = TaskOrchestrator(
            task_store=Mock(),
            logger=Mock(),
            initial_task_concurrency=2,
        )
        fake_executor = _FakeExecutor()
        orchestrator._executor = fake_executor

        orchestrator.submit_download(
            "task-thread",
            "https://telegra.ph/thread",
            timeout=15,
            retries=2,
            image_concurrency=4,
        )

        self.assertEqual(len(fake_executor.calls), 1)
        call_fn, call_args = fake_executor.calls[0]
        self.assertEqual(call_fn, orchestrator._run_download)
        self.assertEqual(
            call_args,
            ("task-thread", "https://telegra.ph/thread", 15, 2, 4),
        )


if __name__ == "__main__":
    unittest.main()

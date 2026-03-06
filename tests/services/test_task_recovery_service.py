import unittest

from telegram_downloader.constants import (
    TASK_RECOVERY_CANCELED_REASON,
    TASK_RECOVERY_FAILED_REASON,
)
from telegram_downloader.services.task_recovery import TaskRecoveryService


class FakeTaskStore:
    def __init__(self, tasks):
        self.tasks = list(tasks)
        self.updates = []

    def list_tasks_with_statuses_older_than(self, cutoff_start_time, statuses):
        _ = cutoff_start_time
        _ = statuses
        return list(self.tasks)

    def update_task(self, task_id, **fields):
        self.updates.append((task_id, fields))


class TaskRecoveryServiceTests(unittest.TestCase):
    def test_recover_stale_active_tasks_marks_terminal_status(self):
        store = FakeTaskStore(
            [
                {"id": "task-in-progress", "status": "IN_PROGRESS"},
                {"id": "task-pending", "status": "PENDING"},
                {"id": "task-cancel", "status": "CANCEL_REQUESTED"},
            ]
        )
        service = TaskRecoveryService(task_store=store, logger=None)

        result = service.recover_stale_active_tasks(now=100)

        self.assertEqual(result["recovered_total"], 3)
        self.assertEqual(result["recovered_failed"], 2)
        self.assertEqual(result["recovered_canceled"], 1)
        self.assertEqual(result["occurred_at"], 100.0)
        self.assertEqual(
            store.updates,
            [
                (
                    "task-in-progress",
                    {
                        "status": "FAILED",
                        "error": TASK_RECOVERY_FAILED_REASON,
                        "result_zip_path": None,
                    },
                ),
                (
                    "task-pending",
                    {
                        "status": "FAILED",
                        "error": TASK_RECOVERY_FAILED_REASON,
                        "result_zip_path": None,
                    },
                ),
                (
                    "task-cancel",
                    {
                        "status": "CANCELED",
                        "error": TASK_RECOVERY_CANCELED_REASON,
                        "result_zip_path": None,
                    },
                ),
            ],
        )


if __name__ == "__main__":
    unittest.main()

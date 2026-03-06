import time

from telegram_downloader.constants import (
    ACTIVE_TASK_STATUSES,
    TASK_RECOVERY_CANCELED_REASON,
    TASK_RECOVERY_FAILED_REASON,
)


class TaskRecoveryService:
    def __init__(self, task_store, logger):
        self._task_store = task_store
        self._logger = logger

    def recover_stale_active_tasks(self, now=None):
        occurred_at = float(now if now is not None else time.time())
        active_tasks = self._task_store.list_tasks_with_statuses_older_than(
            occurred_at + 1e-6,
            list(ACTIVE_TASK_STATUSES),
        )

        recovered_failed = 0
        recovered_canceled = 0

        for task in active_tasks:
            task_id = task.get("id")
            status = (task.get("status") or "").strip().upper()
            if not task_id:
                continue

            if status == "CANCEL_REQUESTED":
                self._task_store.update_task(
                    task_id,
                    status="CANCELED",
                    error=TASK_RECOVERY_CANCELED_REASON,
                    result_zip_path=None,
                )
                recovered_canceled += 1
            else:
                self._task_store.update_task(
                    task_id,
                    status="FAILED",
                    error=TASK_RECOVERY_FAILED_REASON,
                    result_zip_path=None,
                )
                recovered_failed += 1

        recovered_total = recovered_failed + recovered_canceled
        if recovered_total > 0 and self._logger is not None:
            self._logger.warning(
                "Recovered %s stale active task(s) on startup: failed=%s canceled=%s",
                recovered_total,
                recovered_failed,
                recovered_canceled,
            )

        return {
            "recovered_total": recovered_total,
            "recovered_failed": recovered_failed,
            "recovered_canceled": recovered_canceled,
            "occurred_at": occurred_at,
        }

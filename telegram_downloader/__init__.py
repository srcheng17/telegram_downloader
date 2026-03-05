import atexit
import logging
import os
import threading

from flask import Flask

from telegram_downloader.constants import DEFAULT_APP_SETTINGS
from telegram_downloader.repositories.task_store import TaskStore
from telegram_downloader.runtime import AppRuntime
from telegram_downloader.services.log_cleanup import LogCleanupService
from telegram_downloader.services.task_recovery import TaskRecoveryService
from telegram_downloader.services.task_orchestrator import TaskOrchestrator
from telegram_downloader.web.routes import register_routes
from telegram_downloader.web.template_helpers import register_template_helpers


def create_app():
    project_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    app = Flask(
        __name__,
        root_path=project_root,
        template_folder="templates",
        static_folder="static",
    )
    _configure_logging()
    app.secret_key = os.environ.get("SECRET_KEY", os.urandom(24))

    runtime = _build_runtime(app)
    register_template_helpers(app)
    register_routes(app, runtime)
    app.extensions["runtime"] = runtime

    @atexit.register
    def _shutdown_background_workers():
        runtime.task_orchestrator.shutdown()
        runtime.cleanup_service.stop()

    return app


def _configure_logging():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(levelname)s - %(message)s")


def _build_runtime(app):
    tasks_db_path = os.environ.get("TASKS_DB_PATH", os.path.join(app.root_path, "data", "tasks.db"))
    task_store = TaskStore(tasks_db_path)
    app_settings = dict(DEFAULT_APP_SETTINGS)
    settings_lock = threading.Lock()
    execution_backend = (os.environ.get("TASK_EXECUTION_BACKEND", "thread") or "thread").strip().lower()
    if execution_backend not in {"thread", "celery"}:
        app.logger.warning("Unknown TASK_EXECUTION_BACKEND=%s, fallback to thread", execution_backend)
        execution_backend = "thread"

    task_orchestrator = TaskOrchestrator(
        task_store=task_store,
        logger=app.logger,
        initial_task_concurrency=app_settings["task_concurrency"],
        execution_backend=execution_backend,
    )
    recovery_service = TaskRecoveryService(
        task_store=task_store,
        logger=app.logger,
    )

    runtime = AppRuntime(
        task_store=task_store,
        app_settings=app_settings,
        settings_lock=settings_lock,
        task_orchestrator=task_orchestrator,
        cleanup_service=None,
        recovery_service=recovery_service,
    )

    cleanup_service = LogCleanupService(
        task_store=task_store,
        logger=app.logger,
        get_retention_days=lambda: runtime.get_setting("log_retention_days", DEFAULT_APP_SETTINGS["log_retention_days"]),
        get_file_retention_days=lambda: runtime.get_setting(
            "file_retention_days",
            DEFAULT_APP_SETTINGS["file_retention_days"],
        ),
    )
    runtime.cleanup_service = cleanup_service

    try:
        recovery_result = runtime.recovery_service.recover_stale_active_tasks()
        recovered_total = int(recovery_result.get("recovered_total", 0) or 0)
        runtime.set_startup_recovery(
            {
                "happened": recovered_total > 0,
                "recovered_total": recovered_total,
                "recovered_failed": recovery_result.get("recovered_failed", 0),
                "recovered_canceled": recovery_result.get("recovered_canceled", 0),
                "occurred_at": recovery_result.get("occurred_at") if recovered_total > 0 else None,
            }
        )
    except Exception as exc:
        app.logger.warning("Task recovery on startup failed: %s", exc)

    cleanup_service.start()
    return runtime

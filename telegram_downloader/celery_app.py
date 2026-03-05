import os

from celery import Celery


def _env(name, default):
    value = os.environ.get(name)
    if value is None:
        return default
    stripped = value.strip()
    return stripped or default


celery_app = Celery(
    "telegraph_downloader",
    broker=_env("CELERY_BROKER_URL", "redis://redis:6379/0"),
    backend=_env("CELERY_RESULT_BACKEND", "redis://redis:6379/1"),
)

celery_app.conf.update(
    task_default_queue=_env("CELERY_DEFAULT_QUEUE", "downloads"),
    worker_prefetch_multiplier=int(_env("CELERY_WORKER_PREFETCH_MULTIPLIER", "1")),
    task_acks_late=True,
    task_track_started=True,
)

# Ensure tasks are registered when worker boots.
celery_app.autodiscover_tasks(["telegram_downloader"])

# Register explicit task module in case autodiscovery naming differs.
import telegram_downloader.celery_tasks  # noqa: E402,F401

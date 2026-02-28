"""Application-wide constants."""

ALLOWED_TELEGRAPH_HOSTS = {
    "telegra.ph",
    "www.telegra.ph",
    "graph.org",
    "www.graph.org",
}

ACTIVE_TASK_STATUSES = {"PENDING", "IN_PROGRESS", "CANCEL_REQUESTED"}

DEFAULT_LOGS_PER_PAGE = 25
MAX_LOGS_PER_PAGE = 100

DEFAULT_APP_SETTINGS = {
    "task_concurrency": 2,
    "image_concurrency": 2,
    "timeout": 30,
    "retries": 10,
    "log_retention_days": 7,
    "file_retention_days": 7,
}

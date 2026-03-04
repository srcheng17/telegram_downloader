"""Application-wide constants."""

ALLOWED_TELEGRAPH_HOSTS = {
    "telegra.ph",
    "www.telegra.ph",
    "graph.org",
    "www.graph.org",
}

ACTIVE_TASK_STATUSES = {"PENDING", "IN_PROGRESS", "CANCEL_REQUESTED"}
TERMINAL_TASK_STATUSES = {"SUCCESS", "FAILED", "CANCELED"}
KNOWN_TASK_STATUSES = (
    "PENDING",
    "IN_PROGRESS",
    "CANCEL_REQUESTED",
    "CANCELED",
    "SUCCESS",
    "FAILED",
)
STATUS_CATALOG = {
    "PENDING": {
        "label": "等待中",
        "can_cancel": True,
        "can_download": False,
        "terminal": False,
    },
    "IN_PROGRESS": {
        "label": "下载中",
        "can_cancel": True,
        "can_download": False,
        "terminal": False,
    },
    "CANCEL_REQUESTED": {
        "label": "取消中",
        "can_cancel": False,
        "can_download": False,
        "terminal": False,
    },
    "CANCELED": {
        "label": "已取消",
        "can_cancel": False,
        "can_download": False,
        "terminal": True,
    },
    "SUCCESS": {
        "label": "成功",
        "can_cancel": False,
        "can_download": True,
        "terminal": True,
    },
    "FAILED": {
        "label": "失败",
        "can_cancel": False,
        "can_download": False,
        "terminal": True,
    },
}

TASK_RECOVERY_STALE_SECONDS = 30 * 60
TASK_RECOVERY_FAILED_REASON = "服务重启导致下载中断。"
TASK_RECOVERY_CANCELED_REASON = "服务重启期间已完成取消。"

DEFAULT_LOGS_PER_PAGE = 25
MAX_LOGS_PER_PAGE = 100

# Download guardrails to keep single-task resource usage bounded.
MAX_IMAGES_PER_TASK = 300
MAX_IMAGE_BYTES = 25 * 1024 * 1024
MAX_TOTAL_DOWNLOAD_BYTES = 500 * 1024 * 1024
DOWNLOAD_GUARDRAILS = {
    "allowed_domains": tuple(sorted(ALLOWED_TELEGRAPH_HOSTS)),
    "max_images": MAX_IMAGES_PER_TASK,
    "max_image_bytes": MAX_IMAGE_BYTES,
    "max_total_bytes": MAX_TOTAL_DOWNLOAD_BYTES,
}

DEFAULT_APP_SETTINGS = {
    "task_concurrency": 2,
    "image_concurrency": 2,
    "timeout": 30,
    "retries": 10,
    "log_retention_days": 7,
    "file_retention_days": 7,
}

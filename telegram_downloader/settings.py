"""Runtime setting parsing and normalization helpers."""


def clamp_int(value, default, min_value, max_value):
    """Safely parse an int and clamp it to an allowed range."""
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return default
    return max(min_value, min(parsed, max_value))


def normalized_settings(raw_settings, fallback):
    """Normalize settings from forms/session with safe defaults."""
    raw_task_concurrency = raw_settings.get("task_concurrency")
    if raw_task_concurrency is None:
        raw_task_concurrency = raw_settings.get("concurrency")

    raw_image_concurrency = raw_settings.get("image_concurrency")
    if raw_image_concurrency is None:
        raw_image_concurrency = raw_settings.get("concurrency")

    return {
        "task_concurrency": clamp_int(raw_task_concurrency, fallback["task_concurrency"], 1, 10),
        "image_concurrency": clamp_int(raw_image_concurrency, fallback["image_concurrency"], 1, 20),
        "timeout": clamp_int(raw_settings.get("timeout"), fallback["timeout"], 1, 300),
        "retries": clamp_int(raw_settings.get("retries"), fallback["retries"], 0, 20),
        "log_retention_days": clamp_int(
            raw_settings.get("log_retention_days"),
            fallback["log_retention_days"],
            1,
            365,
        ),
        "file_retention_days": clamp_int(
            raw_settings.get("file_retention_days"),
            fallback["file_retention_days"],
            1,
            365,
        ),
    }

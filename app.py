"""Backward-compatible entrypoint for the Flask app."""

from telegram_downloader import create_app
from telegram_downloader.settings import clamp_int, normalized_settings
from telegram_downloader.url_validation import is_allowed_telegraph_url

app = create_app()

runtime = app.extensions["runtime"]
task_store = runtime.task_store
app_settings = runtime.app_settings


def update_task_concurrency(new_task_concurrency):
    runtime.task_orchestrator.update_task_concurrency(new_task_concurrency)


if __name__ == "__main__":
    app.run(debug=True)

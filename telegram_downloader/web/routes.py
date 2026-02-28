import time
import uuid
import os

from flask import jsonify, redirect, render_template, request, send_file, session, url_for

from telegram_downloader.constants import (
    ACTIVE_TASK_STATUSES,
    ALLOWED_TELEGRAPH_HOSTS,
    DEFAULT_LOGS_PER_PAGE,
    MAX_LOGS_PER_PAGE,
)
from telegram_downloader.settings import clamp_int, normalized_settings
from telegram_downloader.url_validation import is_allowed_telegraph_url


def register_routes(app, runtime):
    def _is_safe_download_path(file_path):
        download_root = os.path.realpath(os.environ.get("DOWNLOAD_PATH", "downloaded_images"))
        candidate = os.path.realpath(file_path)
        try:
            return os.path.commonpath([candidate, download_root]) == download_root
        except ValueError:
            return False

    @app.route("/")
    def index():
        return render_template("index.html")

    @app.route("/download", methods=["POST"])
    def download():
        url = (request.form.get("url") or "").strip()
        if not url:
            return redirect(url_for("index"))
        if not is_allowed_telegraph_url(url, ALLOWED_TELEGRAPH_HOSTS):
            app.logger.warning("Rejected URL outside Telegraph domains: %s", url)
            return redirect(url_for("index"))

        settings = normalized_settings(session.get("settings", runtime.app_settings), runtime.app_settings)
        task_id = str(uuid.uuid4())
        runtime.task_store.create_task(
            {
                "id": task_id,
                "url": url,
                "status": "PENDING",
                "start_time": time.time(),
                "error": None,
                "progress": 0,
                "total_images": 0,
                "image_concurrency": settings["image_concurrency"],
                "result_zip_path": None,
            }
        )

        app.logger.info("New task created: %s for URL: %s", task_id, url)
        runtime.task_orchestrator.submit_download(
            task_id,
            url,
            settings["timeout"],
            settings["retries"],
            settings["image_concurrency"],
        )
        return redirect(url_for("logs"))

    @app.route("/logs")
    def logs():
        return render_template("logs.html")

    @app.route("/api/logs")
    def api_logs():
        page = max(1, request.args.get("page", 1, type=int) or 1)
        per_page = clamp_int(
            request.args.get("per_page"),
            DEFAULT_LOGS_PER_PAGE,
            1,
            MAX_LOGS_PER_PAGE,
        )

        paginated_logs, total_logs, total_pages, page = runtime.task_store.list_paginated(page, per_page)
        has_active_tasks = runtime.task_store.has_active_tasks(ACTIVE_TASK_STATUSES)

        return jsonify(
            {
                "logs": paginated_logs,
                "total": total_logs,
                "page": page,
                "per_page": per_page,
                "total_pages": total_pages,
                "has_active_tasks": has_active_tasks,
            }
        )

    @app.route("/api/tasks/<task_id>/cancel", methods=["POST"])
    def cancel_task(task_id):
        task = runtime.task_store.get_task(task_id)
        if task is None:
            return jsonify({"ok": False, "message": "Task not found."}), 404

        status = task.get("status")
        if status in {"SUCCESS", "FAILED", "CANCELED"}:
            return jsonify({"ok": False, "message": f"Task already finished with status {status}."}), 409

        if status == "CANCEL_REQUESTED":
            return jsonify({"ok": True, "message": "Cancellation already requested."}), 200

        runtime.task_store.update_task(
            task_id,
            status="CANCEL_REQUESTED",
            error="Cancellation requested by user.",
            result_zip_path=None,
        )
        return jsonify({"ok": True, "message": "Cancellation requested."}), 202

    @app.route("/api/tasks/<task_id>/download", methods=["GET"])
    def download_task_file(task_id):
        task = runtime.task_store.get_task(task_id)
        if task is None:
            return jsonify({"ok": False, "message": "Task not found."}), 404

        if task.get("status") != "SUCCESS":
            return jsonify({"ok": False, "message": "Task is not completed yet."}), 409

        zip_path = task.get("result_zip_path")
        if not zip_path:
            return jsonify({"ok": False, "message": "Output file not found for this task."}), 404

        if not _is_safe_download_path(zip_path) or not os.path.isfile(zip_path):
            return jsonify({"ok": False, "message": "Stored file is unavailable."}), 404

        return send_file(
            zip_path,
            as_attachment=True,
            download_name=os.path.basename(zip_path),
            mimetype="application/zip",
            conditional=True,
        )

    @app.route("/settings", methods=["GET", "POST"])
    def settings():
        if request.method == "POST":
            current_settings = normalized_settings(request.form, session.get("settings", runtime.app_settings))
            session["settings"] = current_settings

            runtime.update_settings(current_settings)
            app.logger.info("Settings updated: %s", current_settings)
            runtime.task_orchestrator.update_task_concurrency(current_settings["task_concurrency"])
            return redirect(url_for("settings"))

        settings_view = normalized_settings(session.get("settings", runtime.app_settings), runtime.app_settings)
        return render_template("settings.html", settings=settings_view)

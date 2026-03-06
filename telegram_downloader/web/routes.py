import time
import uuid
import os
from urllib.parse import quote

import requests
from flask import jsonify, redirect, render_template, request, send_file, url_for

from telegram_downloader.constants import (
    ACTIVE_TASK_STATUSES,
    ALLOWED_TELEGRAPH_HOSTS,
    DEFAULT_LOGS_PER_PAGE,
    DOWNLOAD_GUARDRAILS,
    KNOWN_TASK_STATUSES,
    MAX_LOGS_PER_PAGE,
    STATUS_CATALOG,
    TERMINAL_TASK_STATUSES,
)
from telegram_downloader.settings import clamp_int, normalized_settings
from telegram_downloader.url_validation import is_allowed_telegraph_url, normalize_telegraph_url


def register_routes(app, runtime):
    def _proxy_v2_request(path):
        base_url = (os.environ.get("GO_BACKEND_BASE_URL") or "http://localhost:5000").strip()
        if not base_url:
            base_url = "http://localhost:5000"
        base_url = base_url.rstrip("/")

        upstream_url = f"{base_url}{path}"
        headers = {}
        content_type = request.headers.get("Content-Type")
        accept = request.headers.get("Accept")
        if content_type:
            headers["Content-Type"] = content_type
        if accept:
            headers["Accept"] = accept

        request_body = None
        if request.method in {"POST", "PUT", "PATCH"}:
            request_body = request.get_data()

        try:
            upstream = requests.request(
                request.method,
                upstream_url,
                params=request.args,
                headers=headers,
                data=request_body,
                timeout=15,
                allow_redirects=False,
            )
        except requests.RequestException as exc:
            app.logger.warning(
                "v2 proxy request failed method=%s path=%s err=%s",
                request.method,
                path,
                exc,
            )
            return jsonify({"error": "upstream unavailable"}), 502

        response_body = b""
        if request.method != "HEAD":
            response_body = upstream.content

        response = app.response_class(response=response_body, status=upstream.status_code)
        for header_name in (
            "Content-Type",
            "Content-Disposition",
            "Cache-Control",
            "ETag",
            "Last-Modified",
            "Content-Length",
        ):
            header_value = upstream.headers.get(header_name)
            if header_value:
                response.headers[header_name] = header_value
        return response

    def _is_safe_download_path(file_path):
        download_root = os.path.realpath(os.environ.get("DOWNLOAD_PATH", "downloaded_images"))
        candidate = os.path.realpath(file_path)
        try:
            return os.path.commonpath([candidate, download_root]) == download_root
        except ValueError:
            return False

    def _claim_download_task(
        url,
        canonical_url,
        image_concurrency,
        reuse_success=True,
        metadata=None,
    ):
        metadata = metadata or {}

        def _new_pending_task():
            return {
                "id": str(uuid.uuid4()),
                "url": url,
                "canonical_url": canonical_url,
                "status": "PENDING",
                "start_time": time.time(),
                "error": None,
                "progress": 0,
                "total_images": 0,
                "image_concurrency": image_concurrency,
                "result_zip_path": None,
                "author": metadata.get("author"),
                "series_name": metadata.get("series_name"),
                "comic_name": metadata.get("comic_name"),
                "summary": metadata.get("summary"),
                "tags_raw": metadata.get("tags_raw"),
                "tags_normalized": metadata.get("tags_normalized"),
                "genres_raw": metadata.get("genres_raw"),
                "genres_normalized": metadata.get("genres_normalized"),
            }

        while True:
            claim = runtime.task_store.claim_download_task(
                _new_pending_task(),
                ACTIVE_TASK_STATUSES,
                reuse_success=reuse_success,
            )
            if claim["decision"] != "reuse_success":
                return claim

            reusable_task = claim["task"]
            zip_path = reusable_task.get("result_zip_path")
            if zip_path and _is_safe_download_path(zip_path) and os.path.isfile(zip_path):
                return claim

            task_id = reusable_task.get("id")
            if not task_id:
                return claim
            runtime.task_store.clear_result_zip_path(task_id)

    def _wants_json():
        if request.is_json:
            return True
        return request.accept_mimetypes["application/json"] > request.accept_mimetypes["text/html"]

    def _coerce_bool(value):
        if isinstance(value, bool):
            return value
        if value is None:
            return False
        return str(value).strip().lower() in {"1", "true", "t", "yes", "y", "on"}

    def _normalize_metadata_text(value):
        if value is None:
            return None
        normalized = str(value).strip()
        return normalized or None

    def _extract_metadata(payload):
        author = _normalize_metadata_text(payload.get("author"))
        series_name = _normalize_metadata_text(payload.get("series_name"))
        comic_name = _normalize_metadata_text(payload.get("comic_name"))
        summary = _normalize_metadata_text(payload.get("summary"))
        tags_raw = _normalize_metadata_text(payload.get("tags"))
        tags_normalized = tags_raw.replace("，", ",") if tags_raw else None
        genres_raw = _normalize_metadata_text(payload.get("genres"))
        genres_normalized = genres_raw.replace("，", ",") if genres_raw else None
        return {
            "author": author,
            "series_name": series_name,
            "comic_name": comic_name,
            "summary": summary,
            "tags_raw": tags_raw,
            "tags_normalized": tags_normalized,
            "genres_raw": genres_raw,
            "genres_normalized": genres_normalized,
        }

    def _extract_download_request():
        if request.is_json:
            payload = request.get_json(silent=True) or {}
            if isinstance(payload, dict):
                return (
                    (payload.get("url") or "").strip(),
                    _coerce_bool(payload.get("force")),
                    _extract_metadata(payload),
                )
            return "", False, _extract_metadata({})
        return (
            (request.form.get("url") or "").strip(),
            _coerce_bool(request.form.get("force")),
            _extract_metadata(request.form),
        )

    def _json_or_redirect_error(message, status_code):
        if _wants_json():
            return jsonify({"ok": False, "message": message}), status_code
        return redirect(url_for("index"))

    def _redirect_after_download_submission():
        return redirect(url_for("index"), code=303)

    def _normalize_status_filter(raw_status):
        normalized = (raw_status or "").strip().upper()
        if normalized in KNOWN_TASK_STATUSES:
            return normalized
        return ""

    def _download_mimetype(file_path):
        extension = os.path.splitext(file_path)[1].lower()
        if extension == ".cbz":
            return "application/vnd.comicbook+zip"
        if extension == ".zip":
            return "application/zip"
        return "application/octet-stream"

    def _build_summary_payload():
        status_counts = runtime.task_store.get_status_counts()
        summary = {
            "total_tasks": int(sum(status_counts.values())),
            "pending_tasks": int(status_counts.get("PENDING", 0)),
            "in_progress_tasks": int(status_counts.get("IN_PROGRESS", 0)),
            "cancel_requested_tasks": int(status_counts.get("CANCEL_REQUESTED", 0)),
            "canceled_tasks": int(status_counts.get("CANCELED", 0)),
            "success_tasks": int(status_counts.get("SUCCESS", 0)),
            "failed_tasks": int(status_counts.get("FAILED", 0)),
        }
        summary["active_tasks"] = (
            summary["pending_tasks"]
            + summary["in_progress_tasks"]
            + summary["cancel_requested_tasks"]
        )
        finished_tasks = (
            summary["success_tasks"]
            + summary["failed_tasks"]
            + summary["canceled_tasks"]
        )
        summary["finished_tasks"] = finished_tasks
        summary["success_rate"] = (
            round((summary["success_tasks"] / finished_tasks) * 100, 1)
            if finished_tasks
            else None
        )
        summary["startup_recovery"] = runtime.get_startup_recovery()
        return summary

    @app.route("/")
    def index():
        return render_template("index.html", download_guardrails=dict(DOWNLOAD_GUARDRAILS))

    @app.route("/v2")
    def v2_index():
        return render_template("v2/index.html")

    @app.route("/v2/tasks-ui")
    def v2_tasks_ui():
        return render_template("v2/tasks.html")

    @app.route("/v2/tasks", methods=["GET", "POST"])
    def v2_tasks_proxy():
        return _proxy_v2_request("/v2/tasks")

    @app.route("/v2/tasks/<task_id>", methods=["GET"])
    def v2_task_detail_proxy(task_id):
        safe_task_id = quote(task_id, safe="")
        return _proxy_v2_request(f"/v2/tasks/{safe_task_id}")

    @app.route("/v2/tasks/<task_id>/cancel", methods=["POST"])
    def v2_task_cancel_proxy(task_id):
        safe_task_id = quote(task_id, safe="")
        return _proxy_v2_request(f"/v2/tasks/{safe_task_id}/cancel")

    @app.route("/v2/tasks/<task_id>/artifact", methods=["GET", "HEAD"])
    def v2_task_artifact_proxy(task_id):
        safe_task_id = quote(task_id, safe="")
        return _proxy_v2_request(f"/v2/tasks/{safe_task_id}/artifact")

    @app.route("/download", methods=["POST"])
    def download():
        url, force_download, metadata = _extract_download_request()
        if not url:
            return _json_or_redirect_error("Please provide a Telegraph URL.", 400)
        if not is_allowed_telegraph_url(url, ALLOWED_TELEGRAPH_HOSTS):
            app.logger.warning("Rejected URL outside Telegraph domains: %s", url)
            return _json_or_redirect_error("Only telegra.ph or graph.org URLs are supported.", 400)

        canonical_url = normalize_telegraph_url(url) or url
        settings = runtime.get_settings_snapshot()
        claim = _claim_download_task(
            url,
            canonical_url,
            settings["image_concurrency"],
            reuse_success=not force_download,
            metadata=metadata,
        )
        decision = claim["decision"]
        selected_task = claim["task"]
        task_id = selected_task["id"]

        if decision == "reuse_success":
            download_url = url_for("download_task_file", task_id=task_id)
            app.logger.info(
                "Duplicate download detected, reusing task %s for URL: %s",
                task_id,
                url,
            )
            if _wants_json():
                return jsonify(
                    {
                        "ok": True,
                        "duplicate": True,
                        "needs_confirmation": True,
                        "task_id": task_id,
                        "download_url": download_url,
                        "force_applied": force_download,
                    }
                )
            return _redirect_after_download_submission()

        if decision == "reuse_active":
            logs_url = url_for("logs")
            app.logger.info(
                "Duplicate active task detected, reusing task %s for URL: %s",
                task_id,
                url,
            )
            if _wants_json():
                return jsonify(
                    {
                        "ok": True,
                        "duplicate": True,
                        "active": True,
                        "task_id": task_id,
                        "logs_url": logs_url,
                        "force_applied": force_download,
                    }
                )
            return _redirect_after_download_submission()

        app.logger.info("New task created: %s for URL: %s", task_id, url)
        runtime.task_orchestrator.submit_download(
            task_id,
            url,
            settings["timeout"],
            settings["retries"],
            settings["image_concurrency"],
        )
        if _wants_json():
            return (
                jsonify(
                    {
                        "ok": True,
                        "duplicate": False,
                        "active": False,
                        "task_id": task_id,
                        "logs_url": url_for("logs"),
                        "force_applied": force_download,
                    }
                ),
                202,
            )
        return _redirect_after_download_submission()

    @app.route("/logs")
    def logs():
        return render_template("logs.html")

    @app.route("/api/summary")
    def api_summary():
        return jsonify(_build_summary_payload())

    @app.route("/api/logs")
    def api_logs():
        page = max(1, request.args.get("page", 1, type=int) or 1)
        per_page = clamp_int(
            request.args.get("per_page"),
            DEFAULT_LOGS_PER_PAGE,
            1,
            MAX_LOGS_PER_PAGE,
        )
        status_filter = _normalize_status_filter(request.args.get("status"))
        query_filter = (request.args.get("q") or "").strip()[:120]

        paginated_logs, total_logs, total_pages, page = runtime.task_store.list_paginated(
            page,
            per_page,
            status=status_filter,
            keyword=query_filter,
        )
        has_active_tasks = runtime.task_store.has_active_tasks(ACTIVE_TASK_STATUSES)

        return jsonify(
            {
                "logs": paginated_logs,
                "total": total_logs,
                "page": page,
                "per_page": per_page,
                "total_pages": total_pages,
                "has_active_tasks": has_active_tasks,
                "filters": {
                    "status": status_filter,
                    "q": query_filter,
                },
                "status_catalog": dict(STATUS_CATALOG),
                "summary": _build_summary_payload(),
            }
        )

    @app.route("/api/tasks/<task_id>/cancel", methods=["POST"])
    def cancel_task(task_id):
        task = runtime.task_store.get_task(task_id)
        if task is None:
            return jsonify({"ok": False, "message": "Task not found."}), 404

        status = task.get("status")
        if status in TERMINAL_TASK_STATUSES:
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

    @app.route("/api/tasks/<task_id>/download", methods=["GET", "HEAD"])
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

        if request.method == "HEAD":
            return "", 200

        return send_file(
            zip_path,
            as_attachment=True,
            download_name=os.path.basename(zip_path),
            mimetype=_download_mimetype(zip_path),
            conditional=True,
        )

    @app.route("/settings", methods=["GET", "POST"])
    def settings():
        if request.method == "POST":
            current_settings = normalized_settings(request.form, runtime.get_settings_snapshot())

            runtime.update_settings(current_settings)
            app.logger.info("Settings updated: %s", current_settings)
            runtime.task_orchestrator.update_task_concurrency(current_settings["task_concurrency"])
            return redirect(url_for("settings"))

        settings_view = runtime.get_settings_snapshot()
        return render_template("settings.html", settings=settings_view)

import os

import requests
from flask import jsonify, request, stream_with_context


def build_go_backend_base_url(env=None):
    env_map = os.environ if env is None else env
    base_url = (env_map.get("GO_BACKEND_BASE_URL") or "http://go-api:5000").strip()
    if not base_url:
        base_url = "http://go-api:5000"
    return base_url.rstrip("/")


def build_proxy_response(app, method, upstream):
    if method == "HEAD":
        response = app.response_class(status=upstream.status_code)
    else:

        @stream_with_context
        def _stream_body():
            try:
                for chunk in upstream.iter_content(chunk_size=64 * 1024):
                    if chunk:
                        yield chunk
            finally:
                upstream.close()

        response = app.response_class(response=_stream_body(), status=upstream.status_code)

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

    if method == "HEAD":
        upstream.close()
    return response


def proxy_v2_request(app, path, requests_module=requests, env=None):
    base_url = build_go_backend_base_url(env)
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
        upstream = requests_module.request(
            request.method,
            upstream_url,
            params=request.args,
            headers=headers,
            data=request_body,
            timeout=15,
            allow_redirects=False,
            stream=True,
        )
    except requests_module.RequestException as exc:
        app.logger.warning(
            "v2 proxy request failed method=%s path=%s err=%s",
            request.method,
            path,
            exc,
        )
        return (
            jsonify(
                {
                    "error": "upstream unavailable",
                    "message": "upstream unavailable",
                    "code": "upstream_unavailable",
                }
            ),
            502,
        )

    return build_proxy_response(app, request.method, upstream)

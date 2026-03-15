import unittest
from flask import Flask
import requests

from telegram_downloader.web.go_proxy import build_go_backend_base_url, build_proxy_response, proxy_v2_request


class _FakeUpstreamResponse:
    def __init__(self, status_code=200, headers=None, chunks=None):
        self.status_code = status_code
        self.headers = headers or {}
        self._chunks = chunks or []
        self.closed = False

    def iter_content(self, chunk_size=65536):
        del chunk_size
        for chunk in self._chunks:
            yield chunk

    def close(self):
        self.closed = True


class GoProxyTests(unittest.TestCase):
    def test_build_go_backend_base_url_defaults(self):
        self.assertEqual(build_go_backend_base_url({}), "http://go-api:5000")
        self.assertEqual(build_go_backend_base_url({"GO_BACKEND_BASE_URL": "  http://x:1/  "}), "http://x:1")

    def test_build_proxy_response_for_head_has_no_body(self):
        app = Flask(__name__)
        upstream = _FakeUpstreamResponse(status_code=200, headers={"Content-Length": "123"})

        response = build_proxy_response(app, "HEAD", upstream)

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.get_data(), b"")
        self.assertEqual(response.headers.get("Content-Length"), "123")
        self.assertTrue(upstream.closed)

    def test_proxy_v2_request_returns_502_when_upstream_unavailable(self):
        app = Flask(__name__)

        with app.test_request_context("/v2/tasks", method="GET"):
            with self.assertLogs(app.logger.name, level="WARNING"):
                response, status_code = proxy_v2_request(
                    app,
                    "/v2/tasks",
                    requests_module=_RaisingRequestsModule(),
                    env={},
                )

        self.assertEqual(status_code, 502)
        self.assertEqual(
            response.get_json(),
            {
                "error": "upstream unavailable",
                "message": "upstream unavailable",
                "code": "upstream_unavailable",
            },
        )


class _RaisingRequestsModule:
    RequestException = requests.RequestException

    @staticmethod
    def request(*args, **kwargs):
        del args, kwargs
        raise requests.RequestException("boom")

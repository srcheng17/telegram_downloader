#!/usr/bin/env python3
"""Provision only the runner-owned, loopback Komga and media workspace."""

import argparse
import base64
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("bootstrap", "configure-app"))
    parser.add_argument("--server", required=True)
    parser.add_argument("--connection-file", required=True)
    args = parser.parse_args()
    server = urllib.parse.urlsplit(args.server)
    if server.scheme != "http" or server.hostname != "127.0.0.1" or not server.port or server.path or server.query or server.fragment or server.username:
        raise RuntimeError("Only the isolated runner loopback origin is accepted")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(method, endpoint, data=None, headers=None, expected=200):
        payload = None if data is None else json.dumps(data).encode()
        req = urllib.request.Request(args.server + endpoint, data=payload, method=method, headers={"Content-Type": "application/json", **(headers or {})})
        try:
            with opener.open(req, timeout=10) as response:
                if response.status != expected:
                    raise RuntimeError(f"Isolated request returned HTTP {response.status}")
                body = response.read(4 << 20)
                return json.loads(body) if body else None
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"Isolated {method} {endpoint} returned HTTP {error.code}") from None

    connection_file = Path(args.connection_file)
    if args.action == "bootstrap":
        deadline = time.monotonic() + 120
        while True:
            try:
                claim = request("GET", "/api/v1/claim")
                break
            except (OSError, RuntimeError):
                if time.monotonic() >= deadline:
                    raise RuntimeError("Isolated Komga readiness timed out") from None
                time.sleep(0.5)
        if claim.get("isClaimed") is not False:
            raise RuntimeError("Refusing to provision an already claimed Komga")
        email, password = "e2e@example.invalid", secrets.token_urlsafe(24)
        request("POST", "/api/v1/claim", headers={"X-Komga-Email": email, "X-Komga-Password": password})
        auth = {"Authorization": "Basic " + base64.b64encode(f"{email}:{password}".encode()).decode()}
        key = request("POST", "/api/v2/users/me/api-keys", {"comment": "isolated synthetic E2E"}, auth)["key"]
        library = request("POST", "/api/v1/libraries", {
            "name": "Synthetic E2E library", "root": "/books", "importComicInfoBook": True,
            "importBarcodeIsbn": False, "scanOnStartup": False, "scanInterval": "DISABLED",
        }, auth)
        if not key or not library.get("id") or library.get("root") != "/books":
            raise RuntimeError("Isolated Komga bootstrap returned invalid configuration")
        with connection_file.open("x") as handle:
            os.chmod(connection_file, 0o600)
            json.dump({"api_key": key, "library_id": library["id"], "base_url": "http://host.docker.internal:25600"}, handle)
        print("Isolated Komga created with synthetic credentials and one mapped library.")
    else:
        connection = json.loads(connection_file.read_text())
        preauth = request("GET", "/api/auth/session")
        session = request("POST", "/api/auth/login", {"password": os.environ["E2E_ADMIN_PASSWORD"]}, {"Origin": args.server, "X-CSRF-Token": preauth["csrf_token"]})
        if not session.get("authenticated"):
            raise RuntimeError("Synthetic administrator login failed")
        headers = {"Origin": args.server, "X-CSRF-Token": session["csrf_token"]}
        previous = request("GET", "/api/settings/komga")
        configured = request("PUT", "/api/settings/komga", {
            "expected_version": previous["config_version"], "base_url": connection["base_url"],
            "credential": {"action": "replace", "value": connection["api_key"]},
        }, headers)
        result = request("POST", "/api/settings/komga/test", {"config_version": configured["config_version"]}, headers)
        if result.get("status") != "connected" or result.get("allowed_library_count") != 1:
            raise RuntimeError("Workspace did not connect to the isolated mapped Komga library")
        print("Workspace administrator API configured and verified isolated Komga connection.")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, KeyError, ValueError) as error:
        # Upstream response bodies and credentials never become runner output.
        if isinstance(error, RuntimeError):
            raise SystemExit(str(error)) from None
        raise SystemExit("Isolated Komga setup failed; check runner-owned inputs.") from None

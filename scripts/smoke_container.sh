#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

smoke_root="$(mktemp -d "${TMPDIR:-/tmp}/panelport-smoke.XXXXXX")"
smoke_project="panelport_smoke_${RANDOM}_${RANDOM}"
export DATA_DIR_HOST="$smoke_root/data"
export KOMGA_DIR_HOST="$smoke_root/komga"
export KOMGA_DIR=""
export APP_UID="$(id -u)"
export APP_GID="$(id -g)"
export APP_PORT
APP_PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
mkdir -p "$DATA_DIR_HOST" "$KOMGA_DIR_HOST"
touch "$smoke_root/empty.env"
cat > "$smoke_root/compose.override.yml" <<EOF
services:
  app:
    ports: !override
      - "127.0.0.1:${APP_PORT}:5000"
EOF

compose() {
  docker compose --env-file "$smoke_root/empty.env" --project-name "$smoke_project" -f "$ROOT_DIR/docker-compose.yml" -f "$smoke_root/compose.override.yml" "$@"
}

cleanup() {
  result=$?
  if [[ "$result" -ne 0 ]]; then
    compose logs --no-color || true
  fi
  compose down --rmi local --volumes --remove-orphans || true
  rm -rf "$smoke_root"
}
trap cleanup EXIT

compose config --quiet
compose up -d --build --wait --wait-timeout 90
python3 - "$APP_PORT" <<'PY'
import io
import json
import os
import struct
import sys
import time
import urllib.error
import urllib.request
import uuid
import zipfile
import zlib
from pathlib import Path
from xml.etree import ElementTree

base = f"http://127.0.0.1:{sys.argv[1]}"
with urllib.request.urlopen(f"{base}/healthz", timeout=5) as response:
    assert response.status == 200
    assert json.load(response)["ok"] is True
for path in ("/", "/static/app.css", "/static/app.js"):
    with urllib.request.urlopen(base + path, timeout=5) as response:
        assert response.status == 200
        assert response.read(), path

settings_request = urllib.request.Request(base + "/api/settings", data=json.dumps({
    "komga_dir": "/komga", "komga_url": "https://komga.example.invalid",
}).encode(), headers={"Content-Type": "application/json"})
with urllib.request.urlopen(settings_request, timeout=5) as response:
    settings = json.load(response)
    assert settings["configured"] is True
    assert settings["komga_dir"] == "/komga"
    assert settings["komga_url"] == "https://komga.example.invalid"

def png(rgb):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(b"\x00" + bytes(rgb))) + chunk(b"IEND", b""))

first, second = png((255, 0, 0)), png((0, 0, 255))
source = io.BytesIO()
with zipfile.ZipFile(source, "w") as archive:
    archive.writestr("pages/02.png", second)
    archive.writestr("pages/01.png", first)
metadata = {
    "author": "容器作者", "series_name": "容器测试系列", "series_number": "2",
    "comic_name": "容器上传验证", "summary": "第一行\n第二行", "tags": "标签甲,标签乙", "genres": "冒险",
}
boundary = "panelport-smoke-" + uuid.uuid4().hex
parts = []
for name, value in metadata.items():
    parts.append((f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n'
                  f'{value}\r\n').encode("utf-8"))
parts.extend([
    (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="fixture.zip"\r\n'
     'Content-Type: application/zip\r\n\r\n').encode(),
    source.getvalue(), f"\r\n--{boundary}--\r\n".encode(),
])
request = urllib.request.Request(base + "/api/jobs/upload", data=b"".join(parts),
                                 headers={"Content-Type": f"multipart/form-data; boundary={boundary}"})
with urllib.request.urlopen(request, timeout=10) as response:
    assert response.status == 202
    job = json.load(response)
job_url = base + "/api/jobs/" + job["id"]
deadline = time.monotonic() + 30
while job["status"] != "succeeded":
    assert job["status"] not in {"failed", "cancelled"}, job
    assert time.monotonic() < deadline, job
    time.sleep(0.1)
    with urllib.request.urlopen(job_url, timeout=5) as response:
        job = json.load(response)
assert job["page_count"] == 2
assert job["status_label"] == "已入库"
assert job["available_actions"] == []
target = Path(os.environ["KOMGA_DIR_HOST"]) / metadata["series_name"] / job["filename"]
assert target.is_file()
cbz = target.read_bytes()
assert len(cbz) == job["size"]
with zipfile.ZipFile(io.BytesIO(cbz)) as archive:
    assert archive.namelist() == ["ComicInfo.xml", "00001.png", "00002.png"]
    assert [archive.read(name) for name in ("00001.png", "00002.png")] == [first, second]
    comic = ElementTree.fromstring(archive.read("ComicInfo.xml"))
    for field, tag in {"author": "Writer", "series_name": "Series", "series_number": "Number",
                       "comic_name": "Title", "summary": "Summary", "tags": "Tags", "genres": "Genre"}.items():
        assert comic.findtext(tag) == metadata[field], tag
for suffix, method in (("/download", "GET"), ("/download", "HEAD"), ("/komga", "POST")):
    request = urllib.request.Request(job_url + suffix, method=method)
    try:
        urllib.request.urlopen(request, timeout=5).close()
    except urllib.error.HTTPError as error:
        assert error.code == 404, (suffix, method, error.code)
    else:
        raise AssertionError(f"Retired route still exists: {method} {suffix}")
data = Path(os.environ["DATA_DIR_HOST"])
assert (data / "app.sqlite3").is_file()
assert not (data / "inputs" / job["id"]).exists()
assert not list((data / "artifacts").iterdir())
print("PanelPort ZIP -> SQLite job -> automatic Komga delivery passed (ordered bytes/XML, 2 pages).")
PY
compose exec -T app python -c 'import gallery_dl; import sqlite3; print("gallery-dl and SQLite available")'
compose exec -T app bsdtar --version
printf 'PanelPort isolated single-container smoke passed.\n'

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export APP_UID="${APP_UID:-$(id -u)}"
export APP_GID="${APP_GID:-$(id -g)}"
local_port="$(docker compose config --format json | python3 -c '
import json
import sys
from pathlib import Path

app = json.load(sys.stdin)["services"]["app"]
for volume in app.get("volumes", []):
    if volume.get("type") == "bind" and volume["target"] in {"/data", "/komga"}:
        Path(volume["source"]).mkdir(parents=True, exist_ok=True)
print(app["ports"][0]["published"])
')"

docker compose up -d --build
docker compose ps
printf '墨渡 PanelPort: http://localhost:%s\n' "$local_port"

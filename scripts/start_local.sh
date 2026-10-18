#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_PORT="${APP_PORT:-5002}"

cd "$ROOT_DIR"
mkdir -p "$ROOT_DIR/downloaded_images" "$ROOT_DIR/temp_downloads" "$ROOT_DIR/data/postgres"

export INTERNAL_ENQUEUE_TOKEN="${INTERNAL_ENQUEUE_TOKEN:-local-dev-token}"
export APP_PORT
export APP_UID="${APP_UID:-$(id -u)}"
export APP_GID="${APP_GID:-$(id -g)}"

docker compose up -d --build
docker compose ps

printf 'Telegraph Downloader is available at http://localhost:%s\n' "$APP_PORT"

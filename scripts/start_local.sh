#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VENV_DIR="${VENV_DIR:-$ROOT_DIR/.venv}"
PYTHON_BIN="${PYTHON_BIN:-python3}"
HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-5000}"

if [ ! -d "$VENV_DIR" ]; then
  "$PYTHON_BIN" -m venv "$VENV_DIR"
fi

"$VENV_DIR/bin/pip" install --upgrade pip
"$VENV_DIR/bin/pip" install -r "$ROOT_DIR/requirements.txt"

mkdir -p "$ROOT_DIR/downloaded_images" "$ROOT_DIR/temp_downloads"

export SECRET_KEY="${SECRET_KEY:-local-dev-secret}"
export FLASK_APP="$ROOT_DIR/app.py"
export APP_ENV="${APP_ENV:-development}"
export DATABASE_URL="${DATABASE_URL:-postgresql://telegraph:telegraph@localhost:5432/telegraph?sslmode=disable}"
export INTERNAL_ENQUEUE_TOKEN="${INTERNAL_ENQUEUE_TOKEN:-local-dev-token}"

exec "$VENV_DIR/bin/python" -m flask run --host "$HOST" --port "$PORT"

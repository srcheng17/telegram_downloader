#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -x "$ROOT_DIR/.venv/bin/pytest" ]]; then
  PYTEST_BIN="$ROOT_DIR/.venv/bin/pytest"
elif [[ -x "$ROOT_DIR/../.venv/bin/pytest" ]]; then
  PYTEST_BIN="$ROOT_DIR/../.venv/bin/pytest"
else
  echo "pytest virtualenv not found (.venv/bin/pytest or ../.venv/bin/pytest)" >&2
  exit 1
fi

(
  cd go-backend
  go test ./...
  go test -race ./...
)

npm run test:frontend
npm run lint
npm run build
PYTHONPATH=. "$PYTEST_BIN" tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
npm run e2e:test

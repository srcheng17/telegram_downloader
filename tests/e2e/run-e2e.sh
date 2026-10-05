#!/usr/bin/env bash
set -euo pipefail

export INTERNAL_ENQUEUE_TOKEN=local-e2e-token
export POSTGRES_DB=taskcore_e2e
export POSTGRES_USER=taskcore_e2e
export POSTGRES_PASSWORD=e2e-test-password
export APP_UID="$(id -u)"
export APP_GID="$(id -g)"

export COMPOSE_PROJECT_NAME="telegraph_e2e_${RANDOM}_${RANDOM}"

if [[ -z "${APP_PORT:-}" ]]; then
  export APP_PORT="$(
    python3 - <<'PY'
import socket

sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
  )"
fi
export E2E_BASE_URL="http://127.0.0.1:${APP_PORT}"
export APP_PUBLIC_ORIGIN="${E2E_BASE_URL}"
export APP_BIND_ADDRESS=127.0.0.1
export ALLOW_INSECURE_LOOPBACK=true
# Synthetic fixture password only; never read deployment credentials for E2E.
export E2E_ADMIN_PASSWORD=e2e-synthetic-admin-password
artifacts_dir="${E2E_ARTIFACTS_DIR:-tests/e2e/.artifacts}"
rm -rf "${artifacts_dir}"
mkdir -p "${artifacts_dir}"

playwright_project="${PLAYWRIGHT_PROJECT:-}"
if [[ -z "${playwright_project}" && "${E2E_CONFIG_ONLY:-0}" != "1" ]]; then
  if ! playwright_project="$(node tests/e2e/browser_preflight.cjs)"; then
    echo "Playwright browser preflight failed before Compose startup." >&2
    exit 1
  fi
fi
export PLAYWRIGHT_PROJECT="${playwright_project}"

temp_docker_config=""
temp_data_root="$(mktemp -d)"
temp_compose_override="$(mktemp)"
compose_started=0
umask 077
mkdir -p "${temp_data_root}/secrets"
export ADMIN_BOOTSTRAP_PASSWORD_FILE_HOST="${temp_data_root}/secrets/admin-bootstrap-password"
export SOURCE_SETTINGS_MASTER_KEY_FILE_HOST="${temp_data_root}/secrets/source-settings-master-key"
python3 - <<'PYSECRET'
import base64
import os
import secrets
from pathlib import Path
Path(os.environ["ADMIN_BOOTSTRAP_PASSWORD_FILE_HOST"]).write_text(os.environ["E2E_ADMIN_PASSWORD"] + "\n")
Path(os.environ["SOURCE_SETTINGS_MASTER_KEY_FILE_HOST"]).write_text(base64.b64encode(secrets.token_bytes(32)).decode() + "\n")
PYSECRET

mkdir -p \
  "${temp_data_root}/downloaded_images" \
  "${temp_data_root}/temp_downloads" \
  "${temp_data_root}/komga" \
  "${temp_data_root}/komga-edit-backups" \
  "${temp_data_root}/telegram-private" \
  "${temp_data_root}/source-retention"

cat > "${temp_compose_override}" <<EOF
services:
  postgres:
    volumes:
      - e2e_postgres:/var/lib/postgresql/data
  go-api:
    environment:
      KOMGA_LIBRARY_ROOT: /app/komga
    volumes: !override
      - ${ADMIN_BOOTSTRAP_PASSWORD_FILE_HOST}:/run/secrets/admin-bootstrap-password:ro
      - ${SOURCE_SETTINGS_MASTER_KEY_FILE_HOST}:/run/secrets/source-settings-master-key:ro
      - ${temp_data_root}/downloaded_images:/app/downloaded_images
      - ${temp_data_root}/temp_downloads:/app/temp_downloads
      - ${temp_data_root}/telegram-private:/app/telegram-private
      - ${temp_data_root}/source-retention:/app/source-retention
      - ${temp_data_root}/komga:/app/komga
      - ${temp_data_root}/komga-edit-backups:/app/komga-edit-backups
  go-worker:
    volumes: !override
      - ${temp_data_root}/downloaded_images:/app/downloaded_images
      - ${temp_data_root}/temp_downloads:/app/temp_downloads
      - ${temp_data_root}/telegram-private:/app/telegram-private
      - ${temp_data_root}/source-retention:/app/source-retention
  gateway:
    ports: !override
      - "127.0.0.1:${APP_PORT}:80"
volumes:
  e2e_postgres: {}
EOF

compose_args=(-f docker-compose.yml -f "${temp_compose_override}")

compose() {
  docker compose "${compose_args[@]}" "$@"
}

if [[ -z "${DOCKER_CONFIG:-}" ]]; then
  temp_docker_config="$(mktemp -d)"
  if [[ -d "${HOME}/.docker" ]]; then
    cp -R "${HOME}/.docker/." "${temp_docker_config}/" 2>/dev/null || true
  fi
  python3 - "${temp_docker_config}/config.json" <<'PY'
import json
import os
import sys

path = sys.argv[1]
data = {}
if os.path.exists(path):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            data = json.load(handle)
    except Exception:
        data = {}

data.pop("credsStore", None)
data.pop("credHelpers", None)

with open(path, "w", encoding="utf-8") as handle:
    json.dump(data, handle)
PY
  export DOCKER_CONFIG="${temp_docker_config}"
fi

cleanup() {
  result_code=$?
  if [[ "$result_code" -ne 0 || "${E2E_CAPTURE_LOGS_ALWAYS:-0}" == "1" ]]; then
    compose ps > "${artifacts_dir}/compose-ps.on-exit.txt" || true
    compose logs --no-color > "${artifacts_dir}/compose-logs.on-exit.txt" || true
  fi
  if [[ "${compose_started}" == "1" ]]; then
    compose down --rmi local --volumes --remove-orphans || true
  fi
  if [[ -n "${temp_docker_config}" && -d "${temp_docker_config}" ]]; then
    rm -rf "${temp_docker_config}"
  fi
  if [[ -n "${temp_compose_override}" && -f "${temp_compose_override}" ]]; then
    rm -f "${temp_compose_override}"
  fi
  if [[ -n "${temp_data_root}" && -d "${temp_data_root}" ]]; then
    rm -rf "${temp_data_root}"
  fi
}
trap cleanup EXIT

if [[ "${E2E_CONFIG_ONLY:-0}" == "1" ]]; then
  compose config --quiet
  docker compose -f docker-compose.yml -f docker-compose.image.yml config --quiet
  echo "Isolated build and image Compose configurations are valid."
  exit 0
fi

compose_started=1
compose up -d --build
compose ps > "${artifacts_dir}/compose-ps.after-up.txt" || true
compose config > "${artifacts_dir}/compose.config.yaml" || true

for i in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${APP_PORT:-5002}/readyz" >/dev/null; then
    break
  fi
  if [[ "${i}" -eq 120 ]]; then
    echo "go-api readiness check timed out" >&2
    exit 1
  fi
  sleep 1
done

playwright_exit=0
npx playwright test --config tests/e2e/playwright.config.ts --project="${PLAYWRIGHT_PROJECT}" "$@" || playwright_exit=$?

exit "${playwright_exit}"

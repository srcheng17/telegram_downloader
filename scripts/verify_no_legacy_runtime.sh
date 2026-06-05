#!/usr/bin/env bash
set -euo pipefail

! test -e app.py
! test -e requirements.txt
! test -d telegram_downloader
! test -d templates
! test -e static/index.js
! test -d static/v2

! rg -n 'REDIS_URL|STREAM_NAME|V2_STREAM_NAME|CONSUMER_GROUP' docker-compose.yml .env.example README.md docs/architecture internal/config tests/e2e/run-e2e.sh >/dev/null
! rg -n '(^|[[:space:]/])redis(:|/|$)' docker-compose.yml tests/e2e/run-e2e.sh >/dev/null
! rg -n 'FLASK_APP|flask run|requirements.txt|app.py' scripts/start_local.sh README.md .github/workflows/ci.yml docker-compose.yml scripts/verify_release_gates.sh >/dev/null
! rg -n 'pytest tests/web|GO_BACKEND_BASE_URL|PYTHON_WEB_BASE_URL' README.md .github/workflows/ci.yml docker-compose.yml scripts/verify_release_gates.sh >/dev/null

#!/usr/bin/env bash
set -euo pipefail

! test -e app.py
! test -e requirements.txt
! test -d telegram_downloader
! test -d templates
! test -e static/index.js
! test -d static/v2
! rg -n 'pytest tests/web|GO_BACKEND_BASE_URL|PYTHON_WEB_BASE_URL' README.md .github/workflows/ci.yml docker-compose.yml scripts/verify_release_gates.sh >/dev/null

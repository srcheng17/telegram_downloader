#!/usr/bin/env bash
set -euo pipefail

[ -f go.mod ]
[ -d cmd ]
[ -d internal ]
[ -d web/templates ]
[ -d web/static/dist ]
rg -n 'module github.com/ryancheng/telegram-downloader' go.mod >/dev/null

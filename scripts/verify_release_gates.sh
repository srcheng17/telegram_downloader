#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

go test ./...
go test -race ./...

npm run test:frontend
npm run lint
npm run build
npm run e2e:test

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -n "${CI:-}" ]]; then
  : "${TEST_DATABASE_URL:?CI requires a PostgreSQL test database}"
fi

go test -p 1 ./... -count=1
go test -race -p 1 ./... -count=1
go vet ./...
python3 scripts/verify_ocr_resources.py
(
  cd tools/tdl-auth-helper
  go test -mod=readonly -race -p 1 ./... -count=1
  go vet -mod=readonly ./...
)

node --test tests/e2e/browser_preflight.test.cjs
npm run test:frontend
npm run lint
npm run build
git diff --exit-code --stat -- web/static/dist
if [[ -n "$(git ls-files --others --exclude-standard -- web/static/dist)" ]]; then
  echo 'Generated frontend bundles must be tracked before release.' >&2
  exit 1
fi
npm run e2e:test

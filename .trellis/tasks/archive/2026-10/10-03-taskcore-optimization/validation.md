# Validation result — 2026-10-03

Baseline: latest remote main `f8f6ee36a6935a4535b0cb51786951c2e06a3727`, reconfirmed with git ls-remote after implementation. Work branch: juvenile-goose.

## Final checks

- `bash scripts/verify_release_gates.sh` with isolated TEST_DATABASE_URL and Chromium browser: PASS (exit 0).
- Full uncached Go tests, race tests and go vet: PASS. Task Core PostgreSQL store tests actually executed against a disposable PostgreSQL 16 database; no business database used.
- Browser preflight checks: 6/6 PASS.
- Frontend Node checks: 74/74 PASS. Same final sources also tested in Node 24 Alpine container: 74/74 PASS; lint passed on Node 24.
- Frontend lint/build and tracked bundle consistency: PASS. Docker built Go 1.27.1 and Node 24 static assets; E2E consumed image resources.
- Isolated Compose Chromium E2E: 11/11 PASS. Real UI ZIP upload -> API/PG/worker -> CBZ download validated exact image bytes and ComicInfo fields -> actual Komga copy.
- git diff --check (worktree and index): PASS.
- npm audit: 0 vulnerabilities (CI agent verified updated lockfile).

## Final regression review

Additional findings were reproduced before fixes: no-source FAILED/CANCELED upload or URL incorrectly offered retry; failed htmx navigation disabled the retained page; completed execution could acknowledge cancellation before artifact deletion. Shared retry qualification, actual-swap guard and consistent cleanup-before-ack now pass focused regressions and the final full gate.

## Scope and release

No architecture replacement. Migration 014 adds generation/settings and ordering index without deleting historical data. Successful upload sources are cleaned only after Complete; failed/canceled sources are retained. No automatic log/artifact retention mechanism is promised.

Remote branch protections were not modified. The user approved the four local work commits on 2026-10-03. No push or merge is included; task archival/journal follows work commits.

Temporary E2E containers/networks/volumes/build images were removed by runner cleanup. The separate review PostgreSQL container and frontend verification tag were also removed and their absence confirmed; unrelated services and shared base images were unchanged.

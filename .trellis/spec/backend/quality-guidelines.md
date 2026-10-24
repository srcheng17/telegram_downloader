# Quality Guidelines

Follow [testing strategy](../../../docs/development/testing-strategy.md). New non-trivial behavior needs a small meaningful test.

```bash
python -m ruff check telegraph_downloader tests
python -m pytest -m 'not e2e'
python -m pytest -m e2e
```

The browser suite uses Python Playwright and the e2e pytest marker. Install requirements-dev.txt and Chromium first. Full gate: `bash scripts/verify_release_gates.sh`. Container/config changes also require `bash scripts/smoke_container.sh`.

Keep tests isolated with temporary SQLite/files/Komga paths and ports. Never test against production data. Cover trust boundaries, process cancellation, persistence and actual CBZ bytes/XML; API mocks do not prove tool integration.

There are no Go, PostgreSQL service, Node, Vite or bundle gates. Report only checks actually executed and explain unavailable browser/Docker validation. Do not run duplicate full suites without a new failure/change.

# Execution

1. Inspect current screenshots and public references; record observations and finalize visual tokens.
2. Refresh shared chrome, icons, typography and CSS; update all three page templates without changing JS contracts.
3. Capture an isolated live preview; inspect desktop, mobile and populated task states; fix visual defects.
4. Run Ruff, non-e2e tests, browser tests, JS syntax and whitespace checks. Have an independent reviewer inspect the changed UI.
5. Record exact results/limits, sync relevant frontend guidelines, commit locally and archive the task. Do not push or deploy.

Validation: `.venv/bin/python -m ruff check telegraph_downloader tests`; `.venv/bin/python -m pytest -m 'not e2e'`; `.venv/bin/python -m pytest -m e2e`; `node --check telegraph_downloader/static/app.js`; `git diff --check`.

Risk areas are source radio keyboard access, long labels/errors, dynamic library/job clones and the previously repaired cross-page gutters/equal-height columns. Preserve these contracts and use existing meaningful tests instead of asserting cosmetic implementation details.

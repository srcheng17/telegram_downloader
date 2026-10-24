# Validation: selected Komga libraries

Verified 2026-10-24 on `refactor/single-host-workspace`.

## Final checks

- `python -m ruff check telegraph_downloader tests`: passed.
- `python -m pytest -m 'not e2e'`: 95 passed, 4 deselected.
- `python -m pytest -m e2e`: 4 passed, 95 deselected. The existing upstream Starlette/httpx deprecation warning remains; no failures.
- `node --check telegraph_downloader/static/app.js`, `bash -n scripts/smoke_container.sh`, and `git diff --check`: passed.
- Screenshots inspected: `test-results/mobile-libraries.png` and `desktop-library-selection.png`; additional real-flow screenshots include `mobile-library-selection.png` and `tasks-libraries.png`.

## Behavior proved

Real ZIP and offline official gallery-dl browser flows deliver separate comics into the selected 国漫/日漫 directory, with the same series name in both roots and no cross-library output. Configuration saves stable IDs and reloads on mobile. Multiple libraries require selection; a sole library selects automatically. Home performs no task-list request before/after submission.

Backend tests cover SQLite v1 saved/env migration, preserved task/cache data, idempotent initialization, unknown historical destination remaining unbound, target snapshots through queueing/config edits/deletion and cached retry, invalid library selections without input/job residue, and canonical same/parent/child/alias path rejection. Library selection does not enter ComicInfo metadata or expose private destination paths in public jobs.

Independent review reproduced and closed a P2: a root or ancestor replaced with a symlink previously allowed a saved 日漫 destination to resolve into 国漫. Selection, execution and copy now reject changed canonical roots. Real ZIP regressions fail safely with no wrong-library CBZ and preserve the cache; restoring the original directory allows retry without regenerating the artifact. Initial explicit settings can still normalize a legitimate symlink entry into its canonical root.

## Container proof

`bash scripts/smoke_container.sh` passed after the final root checks. Image digest: `sha256:5dcd44a17a49fbab8a1c7f08e989ca25a0dac6598fe76e563accd8c1aae7a3f7` (local linux/arm64 Python 3.14).

The isolated container saved two named libraries, uploaded a real two-page ZIP to 国漫, and verified only that directory received the CBZ. It checked exact page bytes/order, all seven Chinese ComicInfo fields, public library identity, SQLite, successful source/cache cleanup, old-route 404 and required runtime tools. All test containers, network and image were removed. Production paths, existing service settings and real Komga were not used.

## Preview and limits

The local preview at `http://127.0.0.1:49360` now runs the new code with fresh temporary data and two example directories (日漫/国漫), zero initial tasks and no real Komga connection. Health, all three pages and CSS/JS returned 200; settings readback contains the two example names.

The corresponding independent libraries must be configured in Komga against the same physical directories. This app does not create Komga libraries, call its scan API, or claim indexing completion. Real Komga indexing and amd64 were not tested in this task. No production deployment, push, PR or merge was performed. Preserve the user's pre-existing `/progress/` ignore edit outside all task commits.

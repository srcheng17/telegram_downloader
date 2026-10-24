# Backend Development Guidelines

Current runtime: Python 3.14, FastAPI, SQLite and one serial background executor in `telegraph_downloader/`. README owns the overview; old Go/PG/Redis plans are historical.

## Before Development

- Read [module boundaries](../../../docs/development/module-boundaries.md), task PRD/design and the relevant guide.
- Trace the affected HTTP, SQLite, runner, media and filesystem boundaries.
- Read [shared thinking guides](../guides/index.md).
- Keep the application single-instance; reuse current store/action semantics.

| Guide | Scope |
| --- | --- |
| [Directory structure](./directory-structure.md) | Small Python module layout |
| [Database](./database-guidelines.md) | sqlite3 and transactions |
| [Errors](./error-handling.md) | API/media failures and cleanup |
| [Logging](./logging-guidelines.md) | Secret-safe diagnostics |
| [Runtime contracts](./task-runtime-contract.md) | Execution, upload, artifacts and restart |
| [Quality](./quality-guidelines.md) | Python/browser/container checks |

Run `python -m ruff check telegraph_downloader tests` and `python -m pytest -m 'not e2e'`. Affected user flows also require browser tests; deployment changes require isolated container smoke.

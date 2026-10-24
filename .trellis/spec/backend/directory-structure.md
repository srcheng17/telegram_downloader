# Directory Structure

| Path | Responsibility |
| --- | --- |
| `telegraph_downloader/__main__.py`, `main.py` | Startup, lifespan, routes, templates/static and HTTP boundaries |
| `config.py` | Paths, fixed tool command and resource limits |
| `store.py` | SQLite jobs/settings, state transitions and public actions |
| `jobs.py` | Serial execution, cancel/retry and restart handling |
| `media.py` | Site tool, archive reader, CBZ/XML and Komga copy |
| `templates/`, `static/` | Chinese UI, CSS and native JS |
| `tests/` | pytest functional/security tests; e2e marker for browsers |

Prefer these small modules to adding domain/app/adapter/repository layers. Media accepts a job/config/cancel/progress callback and returns an artifact; it does not access SQLite. State writes belong to store/runner, and pages consume backend state/actions.

Keep paths under DATA_DIR; a configured Komga root is the only extra write destination. Read [module boundaries](../../../docs/development/module-boundaries.md) before moving responsibilities.

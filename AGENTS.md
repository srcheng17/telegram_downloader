<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## Project conventions

- Runtime: Python 3.14 FastAPI, local SQLite, one serial background executor and one application instance. Frontend: Jinja templates, CSS and native JavaScript. See `README.md` for the current layout.
- Read `docs/development/module-boundaries.md` and the relevant `.trellis/spec/backend/` or `.trellis/spec/frontend/` index before editing a layer.
- Current task states and action eligibility live in `telegraph_downloader/store.py` and the public job API. Older Go/PostgreSQL/Redis documents are historical; resolve discrepancies against current code.
- UI text is Chinese. Templates and static source under `telegraph_downloader/` are served directly; there is no Node/Vite build or generated bundle requirement.
- Product: 墨渡 PanelPort. Home is submit-only, task history and Komga settings have separate pages. Jobs automatically enter Komga; there are no browser download or manual-copy routes. Keep the Python module and existing GHCR path unchanged unless explicitly migrating them.
- Delegate site downloads to pinned gallery-dl and RAR/7Z handling to libarchive. Keep filesystem containment, upload/archive limits, subprocess cancellation and atomic artifact publication.
- Use isolated temporary data and Komga directories for tests. Never migrate/delete old production data or operate production services as a side effect of development.
- Run checks appropriate to the changed layer, following its spec index. Report only checks actually run and their results.
- Follow the English Conventional Commits instructions in `paseo.json`.
- Trellis session and task bookkeeping does not auto-commit; review and commit it with the intended work.

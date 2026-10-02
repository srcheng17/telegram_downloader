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

- Runtime: Go API and worker, PostgreSQL Task Core, nginx; frontend: native JavaScript, htmx, Go templates, Vite. See `README.md` for the current layout.
- Read `docs/development/module-boundaries.md` and the relevant `.trellis/spec/backend/` or `.trellis/spec/frontend/` index before editing a layer.
- Current task states and action eligibility are defined in `internal/domain/taskcore/`; older architecture documents can describe retired states or Redis paths. Resolve discrepancies against the current code.
- UI text is Chinese. Frontend source changes must include rebuilt `web/static/dist/*.bundle.js` artifacts.
- Run checks appropriate to the changed layer, following its spec index. Report only checks actually run and their results.
- Follow the English Conventional Commits instructions in `paseo.json`.
- Trellis session and task bookkeeping does not auto-commit; review and commit it with the intended work.

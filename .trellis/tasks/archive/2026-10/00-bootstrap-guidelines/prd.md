# Trellis Codex Initialization

## Request

Initialize Trellis for Codex in the telegram-downloader project.

## Acceptance Criteria

- [x] Initialize Trellis 0.6.17 with Codex project skills, agent profiles, hooks, and root AGENTS.md.
- [x] Use the existing isolated worktree and preserve existing business files and Paseo configuration.
- [x] Populate backend and frontend specifications from current Go/PostgreSQL and native JavaScript/htmx code.
- [x] Include real source examples and references to existing project documents.
- [x] Disable automatic journal/task Git staging and commits.
- [x] Verify actual Codex instruction/skill discovery and runnable Trellis context/hook scripts.

## Verification Results

- Codex 0.159.3 `debug prompt-input`: project AGENTS.md and all 12 Trellis skills discovered.
- Codex official skill frontmatter validator: all 12 skills passed.
- All 13 backend/frontend spec documents populated; index sections and relative links verified.
- All generated Python scripts/hooks compile with Python 3.9.6.
- Direct UserPromptSubmit hook smoke check: valid workflow/bootstrap context JSON.
- `task.py validate 00-bootstrap-guidelines`: implementation and check context manifests passed.
- `get_context.py --mode packages`: backend and frontend layers detected.
- Developer identity, session runtime, and Python caches ignored by Git.
- `get_session_auto_commit`: false. Existing tracked project files unchanged.
- Independent review: no issues found.
- Application tests and deployment: not run; changes are workflow configuration and documentation only.

## Usage

Start a fresh Codex session in this worktree and invoke `$trellis-start`.
The user-level hooks feature is already enabled; review newly installed hooks
through `/hooks` before relying on automatic context injection. Skills and
agent profiles retain their explicit context-loading fallback.

# Frontend skill selection and maintenance

Verified 2026-10-24 using GitHub repository metadata and immutable upstream file bytes:

| Candidate upstream | Repository stars |
| --- | ---: |
| anthropics/skills (frontend-design) | 183319 |
| nextlevelbuilder/ui-ux-pro-max-skill | 139475 |
| Leonxlnx/taste-skill | 101653 |
| pbakaus/impeccable | 85591 |
| emilkowalski/skills | 49102 |
| vercel-labs/agent-skills | 32902 |
| 21st-dev/magic-mcp | 6217 |

These counts belong to repositories, not an individual skill. Selected Anthropic frontend-design; its per-skill license is Apache-2.0. Canonical directory ~/.agents/skills/frontend-design is already installed and byte-identical to upstream SKILL.md and LICENSE.txt. Registered immutable revision41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f remains valid; current upstream HEAD dbd4588f9e1033efb41dad4bef2f7947c8993d44 has identical skill bytes. Reinstalling identical bytes would add no capability.

User authorized removal of other frontend design skills. Backed up config.toml, source registry, selected skill, UI UX Pro Max2.13.0 and Paseo Progress links in ~/.agents/skill-backups/frontend-rebuild-20261024T110610. Removed plugin through `codex plugin remove ui-ux-pro-max@ui-ux-pro-max-skill --json`, withdrawing its seven bundled design skills. Did not delete testing, accessibility or vendor application sources.

A prerequisite strict audit found an unregistered vendor-owned Paseo Progress1.8.0 link and duplicate Codex exposure. Registered its canonical vendor link and moved the duplicate Codex link into the same backup. The plugin Install skill button may recreate that duplicate later. Strict audit now passes without errors/warnings; frontend-design official quick validator passes; reconciliation preview has no changes or conflicts.

Runtime discovery verified with codex debug prompt-input: frontend-design appears exactly once; UI UX Pro Max design entries0 and plugin absent.

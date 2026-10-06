# MiniCPM through CPA

## Current user requirement (supersedes direct MiniCPM acceptance)

Keep MiniCPM on RackNerd. Register it as a CPA upstream and configure the media management workspace to use CPA to select and extract with MiniCPM. The chain is management workspace → existing CPA → MiniCPM. Stop the abandoned MiniCPM Tailnet work; do not use SSH for traffic or management. The user explicitly authorized the assistant to complete synthetic sending and evidence review directly; do not ask again for YES, REUSE or keys.

## Boundaries

Preserve deployed Telegram and Komga connections and the existing schema/fixture repairs. Keep current credentials secret and reuse them in memory through native control planes. The original operational phase excluded commits, pushes and merges. Source changes are now included in the subsequent review submission; merging remains separately authorized. Add a narrowly supported llama.cpp OpenAI chat protocol through CPA, explicitly selected in saved settings, while preserving native protocol behavior for existing saved configurations. No automatic fallback, model switch, source/output trimming, or weaker field/evidence validation.

## Acceptance

- CPA models lists minicpm5-2b-q4 and an actual synthetic completion passes through CPA.
- Saved AI protocol, destination and model can be configured in CLI and management UI; old configurations retain native semantics.
- The chat adapter validates pinned build, exact selected model, complete response and truthful budget mode; it does not claim native exact token preflight.
- Narrow tests, required backend/frontend/CLI gates and independent review pass. Skipped PG tests are recorded as skipped.
- Backed-up native Dockhand deployment is verified at runtime and other services remain stable.
- The real workspace extraction returns title exactly 验收示例 with correct UTF-16 evidence; AI remains enabled, ephemeral workspace is closed.
- Telegram verify and Komga connection/library access still pass.

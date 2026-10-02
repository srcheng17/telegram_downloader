# Task Core runtime optimization

User approved implementation of the 2026-10-03 architecture/CI review against latest main f8f6ee3.

Acceptance:
- Independent task runs never overwrite artifacts; cancellation stops execution before acknowledgment, with real progress and execution fencing.
- URL tasks atomically reuse active tasks, confirm existing successful artifacts and honor force. Settings for new tasks are persisted and consumed by the worker.
- Upload extraction enforces image count/size/total limits, cancellation and bounded disk use. Invalid uploads fail without leaking files.
- Logs filter/count/page in PostgreSQL with no 10000-row cutoff.
- Komga failures, non-JSON upload failures and htmx history restore behave correctly. Unsupported concurrency/retention controls are removed with accurate UI/docs.
- CI runs actual PostgreSQL tests and a real ZIP upload/worker/download test with isolated writable volumes. Build outputs are reproducible; images include static assets; supported toolchains are used.
- Focused regression checks and complete Go/race/vet/frontend/lint/build/E2E verification pass.

No merge into main. Remote repository protections are reported separately from code changes; no speculative architecture rewrite or bulk deletion of migration history.

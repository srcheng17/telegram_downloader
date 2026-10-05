# Batch 2 integration boundary

Implementation resumed with user approval on 2026-10-05. Batch 1 and earlier runtime fixes remain intact and uncommitted.

The behavior gap is that the protected workspace has metadata editing and configuration, but its OCR, provider, Telegram and archive round-trip slots are not yet functional. Business rules stay in application/domain packages; root wires the existing composition roots, HTTP router, Task Core input/worker paths, page lifecycles and deployment assets.

Root owns `cmd/server`, `cmd/worker`, shared HTTP routing/configuration, existing Task Core and archive pipeline integration, frontend page entries/templates/styles, package locks, Vite output, migration numbering and cross-layer tests. Root also owns extraction-rules application/store/HTTP and the saved AI snapshot/model transport adapter. Workers own their dedicated OCR, provider and Telegram modules; ComicInfo starts as the next slot becomes available. Provider worker may extend only domain provenance attribution fields and their validation.

No lifecycle redesign, framework migration, production deployment or persistent model tunnel is included. No change may reset prior work. Shared behavior must retain authentication, CSRF, transient evidence, immutable task input and fenced publication. Migration 017 already belongs to metadata-history identity; Telegram starts at 018.

Validation progresses from focused unit and isolated PostgreSQL tests to frontend build and real browser/worker integration. Mock, public protocol, actual account and actual model checks are recorded separately. Real account/model access is never inferred from mocked success.

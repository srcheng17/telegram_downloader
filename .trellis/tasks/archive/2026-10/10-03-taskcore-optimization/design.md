# Design

Retain the existing Go/PostgreSQL/htmx architecture. Reuse downloader callbacks, domain transitions, repository transactions and current frontend lifecycle modules.

Use a monotonic execution generation separate from each retry budget. Condition heartbeat/progress/terminal writes on the claimed execution. Isolate physical artifact paths while preserving display names. Store supported download settings as part of task input.

Serialize URL creation by canonical URL inside the database transaction; reuse active tasks and surface existing successful results via the existing confirmation contract. Filter/paginate/count using SQL.

Bound archive reading rather than allocating unbounded contents. External archives must not be fully expanded to disk before limits are enforced. Make CBZ publication cancellation-aware.

Keep mock browser tests for UI branches and add a real self-contained upload fixture exercising storage, worker and CBZ contents. CI gets an isolated PostgreSQL service. Build bundles from source and verify tracked outputs; Docker carries generated static assets.

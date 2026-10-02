# Implementation and validation

1. Backend worker agent: app/taskcore, postgres/taskcore, domain/taskcore, worker/taskcore, cmd/worker, CBZ writer. Own settings snapshots, URL dedupe, generation/progress/cancellation and DB querying. Publish interface changes to root promptly.
2. Frontend agent: frontend sources/tests and web templates. Own errors/history/unsupported fields; build bundles after all frontend changes.
3. CI agent: workflow, Docker/Compose/E2E scripts and tests, toolchain baselines. Own isolated test database and real upload regression. No remote protection writes.
4. Root: archive extraction, HTTP request/response adaptation, artifact access, server wiring and docs. Integrate interfaces and inspect whole diff.
5. Run focused checks, then real PG tests, Go/race/vet, Node/lint/build and full isolated Compose E2E. Review cross-layer changes and update specs; commit only verified results. Do not merge.

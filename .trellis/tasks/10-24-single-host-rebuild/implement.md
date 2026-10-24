# Implementation

1. Audit skill catalog, verify upstream stars/licenses, review immutable frontend skill, back up and replace/retire design entries; repair only prerequisite provenance issues.
2. Implement Config/Store/Runner/Web and focused lifecycle, API/path tests. Implement media executor and its fixture tests independently under the documented contract.
3. Implement complete responsive Chinese templates/CSS/JS using selected skill and public API contract.
4. Replace one-service deployment, Python dependencies/CI/release gates and current docs/specs. Retire old Go/PG/Vite code and old-only gates/tests, preserve data.
5. Run Python lint/unit/integration, actual gallery fixture and archive tests, browser desktop/mobile E2E and isolated container checks where available. Review/fix failures then rerun only affected checks.
6. Record actual outcomes, skill provenance/backup, screenshots and migration limitations; local Conventional Commit after review. Do not push or merge without a user request.

## Validation
pytest, ruff check, browser tests against isolated temp directories, Docker build/one-service smoke with no production volumes, archive/XML bytes and Komga copy assertions. Baseline old Go and frontend checks are reference only, new gate must validate Python runtime.

## Final steering
Home/history/settings are separate; no browser download or manual copy endpoint. Settings configure shared directory and optional Komga website. Delivery is a noncancellable commit phase, with cache retained on failure and removed only after registered success. Re-run the affected API/browser/container checks against this final flow.

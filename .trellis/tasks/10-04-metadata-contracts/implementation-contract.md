# Batch 1 implementation handoff

Implementation and focused checks completed locally on 2026-10-04. Parent integration owns task completion, full release checks and generated bundles; this note does not advance task state.

## Public Go / JSON contract

- `metadata.StandardRegistry() Registry`: fixed `standard-v1`, definitions map. `NewRegistry(previous, custom)` creates immutable content-addressed custom versions. Keep the standard-v1 definitions stable when introducing a future built-in version.
- `metadata.Document`: schema/definitions versions, revision, fields map and exact used-definition snapshot. Values are typed `json.RawMessage`; validation rejects unknown/duplicate JSON members, null revisions/locks, unsupported versions, forged snapshots, invalid types and budgets.
- `metadata.DecodeJSON(data, destination)`: strict boundary decoder, including duplicate members anywhere and `json.Number` preservation in generic envelopes. `Decode(data, registry)` and `Validate(document, registry)` provide full document validation.
- `ApplyPatch(document, registry, expectedRevision, operations, MergeContext)`: one document revision per atomic batch. Operations are set/clear/unlock/adopt; manual set/clear locks values. Adoption checks selected field revisions plus input/config revision, preserves existing locks, and cannot create a lock or manual provenance. `confirm_locked` permits an explicit replacement without unlocking. No persistent draft CAS exists.
- `MetadataCandidate` / `CandidateField` are the sole candidate contract. `ValidateCandidate` validates source extraction capability. `CandidateFromDocument` converts history only within the same definition version; full history clones retain their saved version. Cross-version migration is explicit, not inferred.
- `FromLegacy(Legacy)`, `ToLegacy(Document)`, `CheckLegacy(Document, Legacy)`: seven-field compatibility. Pointers distinguish omitted legacy input from supplied empty input. Mixed input normalizes **only legacy values** before comparing canonical values; it never re-splits new array items to hide semantic conflicts. A multi-word tag or comma-containing creator can be inherently lossy in old clients; new clients submit only the document.
- `appmetadata.NewService(Repository)` exposes `Schema`, `Fields`, `UpdateFields`, `Validate`, `Patch`, `Decode`, `Encode`, `NormalizeInput(ctx, *Document, Legacy) (Document, Legacy, error)`.
- Settings GET/PUT result: `{definitions_version, definitions: FieldDefinition[], limits}`. PUT input: `{expected_definitions_version, definitions}`. Existing custom keys cannot be removed or change type; disable preserves old definitions.
- Errors support `errors.Is` for `ErrInvalidInput`, `ErrConflict`, and `ErrUnsupportedVersion`; `ValidationError.Fields` supplies field/code pairs. Warnings use `{key, code, message}`.
- `metadatadoc.NewStore(pool)` implements Current/Get/Save with immutable version rows and a locked singleton head. Lazy initial standard-v1 insertion is persisted, not an in-memory fallback. `DecodeStored` upgrades only NULL legacy records.

Shared language-neutral fixture: `internal/domain/metadata/testdata/contract-v1.json`, checked by `TestSharedContractFixture`. It contains registry, empty/extended documents, candidate, patch and expected result. Intentional updates use `UPDATE_METADATA_FIXTURES=1` and require notifying consumers.

## Additional delegated integration work

- `createMetadataFieldsModule({doc, api})` in `frontend/src/settings/metadata_fields.js` returns mount/unmount. It uses injected getJson/putJson, server limits, optimistic definition versions, explicit disable, safe DOM rendering and owned request cancellation. Root mounts it in the metadata-fields slot and builds dist.
- Task JSON envelopes now inspect bounded original bytes before map decoding, preserving duplicate-key rejection and integer precision for download and upload-init.
- `cmd/server/workspace_test.go` uses a disposable PostgreSQL schema per test. It exercises initialization fail-closed, administrator/CSRF, public nested login chunks, custom definitions CAS, thirteen-field patch, atomic task/history persistence, mixed-input refusal, NULL legacy upgrade, upload-init, cancel acknowledgment/retry, worker claim generations, and separate submitted/effective history.
- `cmd/server/model_contract_test.go` validates the nine-field model test fixture against persisted registry state. The production adapter binds schema/definitions/fixture versions and selected keys and validates output through canonical MetadataCandidate.

## Verification run

- `go test ./internal/domain/metadata ./internal/app/metadata ./internal/store/postgres/metadatadoc -count=1` with isolated PostgreSQL `metadata_test`: passed.
- `go test ./internal/httpapi -count=1`: passed.
- `go test ./cmd/server -count=1` with isolated PostgreSQL `server_test`: passed.
- Focused race run across metadata domain/app/store, HTTP and cmd/server: passed with independent metadata/server databases.
- `go vet` across those five packages: passed.
- `node --test frontend/src/tests/metadata_fields.test.mjs`: 5 passed.
- `npx eslint frontend/src/settings/metadata_fields.js`: passed.

## Handoff review

Current definitions stay immutable and available after custom disable; application writes verify exact snapshots. Task/input/history creation is transactional. Effective snapshots are written only after the current lease/attempt/generation terminal fence, in the same transaction; stale completions cannot replace a successful effective snapshot. Existing retry deletes only an obsolete result and retains submitted input.

Batch 2 must route effective-document production through a validated application use case that checks its definition version/snapshot against submitted input before Complete. The current optional CompleteInput storage slot only JSON-encodes a trusted caller's document, and no production effective-document producer is enabled in Batch 1. Packaging must consume Document directly to retain multi-word array values and extended fields instead of treating the seven-field projection as authoritative.

## Final contract corrections and read validation

- Empty `string[]`, `identifiers` and custom list values are valid `state=value`, retain revisions/locks/provenance and remain distinct from absent or `cleared`. JSON `null` and empty strings remain invalid values. Added manual/candidate/legacy and actual JSONB roundtrip coverage. Legacy cannot express an explicit empty-list fact; omitted compatibility input is allowed, but supplied legacy empty strings (absent semantics) conflict with document `[]` instead of changing that fact.
- Added `metadata.DecodeStored(data)` and wired Task Core submitted/effective reads and metadata-history submitted/effective reads through it. Future schema versions, unsafe/altered standard definitions, invalid custom definitions, mismatched value types and incomplete snapshots fail closed before compatibility projection or worker consumption. Current settings never reinterpret old snapshots.
- Stored custom snapshots contain only used definitions, so this local read validator cannot independently recompute the full registry hash. Normal writes continue verifying the immutable persisted registry. Built-in standard-v1 definitions are compared exactly; this stored decoder is not an alternative client-input validator.
- Final verification after these corrections: seven affected packages (`domain/metadata`, `app/metadata`, `store/postgres/metadatadoc`, `store/postgres`, `store/postgres/taskcore`, `httpapi`, `cmd/server`) passed `go test -race -p 1 ... -count=1` with isolated real PostgreSQL databases, and `go vet` passed. No further source edits planned before the parent's final release gate.

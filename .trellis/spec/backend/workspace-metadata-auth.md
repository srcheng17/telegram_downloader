# Workspace metadata and administrator contracts

## 1. Scope / Trigger

Use for metadata documents, custom definitions, task snapshots, administrator access,
source credentials or model settings. Storage/configuration contracts below are
shared by the extraction, provider, Telegram and packaging modules; see the index
for their protocol-specific specs.

## 2. Signatures

- `GET /api/metadata/schema` returns `Registry`; `definitions` is a map keyed by field ID.
- `GET/PUT /api/settings/metadata-fields` returns custom definition array and limits;
  PUT requires `expected_definitions_version` and the full `definitions` array.
- `POST /api/metadata/validate`, `/api/metadata/patch` are stateless draft operations.
- Task creation accepts `metadata_document`; form transport uses a JSON string.
  `app/metadata.Service.NormalizeInput` validates and produces the legacy projection.
- `GET /api/auth/session`, `POST /api/auth/login`, `/logout`, `PUT /api/auth/password`.
- Source and AI settings handlers live in `internal/httpapi/source_settings.go`;
  services own CAS/credential actions, `internal/modelapi` owns bounded HTTP calls.
- Migrations 015/016/017 add document/registry/auth/settings storage. Never rewrite
  001–014 or use an old seven-field unique index for new task history.

## 3. Contracts

`Document` contains `schema_version`, `definitions_version`, `revision`, `fields`,
`definition_snapshot`. Field states distinguish absent, `value`, and `cleared`;
revisions/manual locks/provenance survive task, history and retry round trips.
Non-nil empty arrays, false and zero are values, never shorthand for absent/cleared;
null is rejected. Empty legacy text is absent and cannot silently equal a typed [].
Use the registry limits (256 KiB document, at most 64 custom fields), not UI copies.
Custom IDs are `custom.user.<name>`; stored keys/types cannot be changed or deleted.
Stop using a field by disabling it. Old documents retain their original definitions.

Decode original JSON bytes via `metadata.DecodeJSON` before mapping: it rejects
repeated nested keys, unknown typed fields, invalid UTF-8 and trailing JSON, and
preserves map numbers. Do not decode into a map and re-marshal before this check.
`metadata_document` is authoritative. Legacy-only input uses `FromLegacy`; mixed
input must compare normalized legacy values against the document's typed values.
For example, one tag `Slice of Life` must not equal three legacy space-split tags.

Task/input/progress/history creation is one transaction. Linked history is unique
by `task_id`; legacy unlinked rows retain the old dedup index with `task_id IS NULL`.
Do not collapse tasks whose seven-field projections match but extended fields differ.
Retry preserves input; effective metadata is separate and stored only on fenced
completion. Packaging produces an effective document with the submitted definitions
version, validated again before fenced completion. Retention paths stay private;
the public task response exposes the profile and finite metadata warnings only.

Root middleware protects every business route, including old `/v2/*`, uploads and
file GET/HEAD. Only login/session, minimal health probes and exact shipped static
JS/CSS paths are public. Vite shared JS chunks under `dist/assets/` must also be listed.
Mutations require same-origin Origin/Referer and session CSRF. Internal tokens never
bypass login. Cookies are HttpOnly/SameSite Strict, Secure for HTTPS; session absolute
TTL is 12h and idle TTL 30m. Password changes revoke all sessions.

`APP_PUBLIC_ORIGIN` is required. HTTP needs explicit `ALLOW_INSECURE_LOOPBACK=true`
and a loopback origin. Bootstrap/password and master-key files use owner-only modes,
regular files and no symlink traversal. The master key stays outside PostgreSQL.
AES-GCM credential reads expose only configured flags. `keep/replace/clear` are
explicit; changing AI base URL cannot keep an old key. Model calls never follow
redirects and only fixed neutral test text is used by the settings test endpoint.
See `docs/development/admin-access.md` for recovery/rotation and deployment details.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Bad document/type/duplicate JSON key | 400, no write |
| Stale metadata definition or candidate revision | conflict, no overwrite |
| Source/AI settings CAS mismatch | 409, keep current state |
| Missing required settings version or invalid config | 422 |
| Anonymous/expired session | API 401; HTML navigation redirects to login |
| Missing/wrong CSRF or wrong Origin | 403 |
| Missing bootstrap/master key or unsafe secret file | startup fails closed |
| Wrong master key, tamper, model redirect/oversize response | bounded generic failure; no secret output |

## 5. Good / Base / Bad Cases

Good: submit 12 fields and an explicit clear; immutable document and history match.
Base: submit only legacy title; deterministic v1 document and legacy response agree.
Bad: duplicate nested field value, reuse stale settings version, or send prior AI key
to a changed target; reject without writing or following redirects.

## 6. Tests Required

Use shared `internal/domain/metadata/testdata/contract-v1.json` for Go/JS parity.
Real PG tests cover concurrent registry/CAS/bootstrap, ciphertext isolation,
atomic task/history rollback, identical legacy metadata in different tasks,
retry snapshots and stale completion fencing. Server tests cover root auth/static
chunks, CSRF, upload and document/history round trips. Run full Go tests and race.
Serial packages (`go test -p 1`) or isolated databases/schemas avoid fixed test-row
collisions; do not treat skipped PG tests as persistence verification.

## 7. Wrong vs Correct

Wrong: write a task then best-effort HTTP history, or put a token in metadata/HTML.
Correct: let Task Core create the task and history atomically; source secrets stay
in encrypted settings and are resolved only for the selected adapter/server call.

# Editing an existing Komga CBZ

## 1. Scope / Trigger

Use this contract for the existing-book flow in `internal/app/komgaedit`,
`internal/archive/cbzedit`, `internal/infra/komga`, its HTTP adapter, deployment
configuration, or operation persistence. This is separate from task output
packaging in [metadata packaging](./metadata-packaging.md): an existing CBZ must
keep every unedited member and its order. Komga is a later, asynchronous
projection of the file. Neither a Komga PATCH nor an accepted analyze request
is the file save.

## 2. Signatures

```go
// internal/app/komgaedit
func (s *EditService) Detail(ctx context.Context, bookID string) (EditDetail, error)
func (s *EditService) Preview(ctx context.Context, bookID string, req PreviewRequest) (PreviewResult, error)
func (s *EditService) Save(ctx context.Context, bookID string, req SaveRequest) (OperationView, error)
func (s *EditService) Operation(ctx context.Context, operationID string) (OperationView, error)
func (s *EditService) RetrySync(ctx context.Context, operationID string) (OperationView, error)
func (s *EditService) Restore(ctx context.Context, operationID string) (OperationView, error)

// internal/archive/cbzedit; sourceRoot and backupRoot are application-owned os.Root values
func Inspect(ctx context.Context, sourceRoot *os.Root, relativePath string, limits Limits) (Inspection, error)
func Prepare(ctx context.Context, sourceRoot, backupRoot *os.Root, req PrepareRequest) (Prepared, error)
func Commit(ctx context.Context, sourceRoot, backupRoot *os.Root, prepared Prepared) (CommitResult, error)
func Probe(ctx context.Context, sourceRoot, backupRoot *os.Root, relativePath, operationID, originalSHA string) (ProbeResult, error)
func Restore(ctx context.Context, sourceRoot, backupRoot *os.Root, prepared Prepared) (RestoreResult, error)
```

HTTP routes: `GET /api/komga/libraries`, `GET /api/komga/books` (required
`library_id`, optional `query`, `page`, `size`), `GET /api/komga/books/{id}`,
`GET /api/komga/books/{id}/edit`, `POST /api/komga/books/{id}/preview`,
`POST /api/komga/books/{id}/save`, `GET /api/komga/edits/{operationId}`,
`POST /api/komga/edits/{operationId}/sync`, and `/restore`. Komga connection
uses `GET/PUT /api/settings/komga` and `POST /api/settings/komga/test`.
Migration [023](../../../internal/store/postgres/migrations/023_komga_edit.sql)
adds encrypted `komga_connection`, versioned `komga_edit_operations`, a unique
idempotency key, and a partial unique active `(library_id, relative_path)` index.
`OperationStore.WithBookLock` uses a PostgreSQL session advisory lock across
instances; `Update` compares the operation version.

## 3. Contracts

- `KOMGA_LIBRARY_MAPPINGS` is a deployment-owned JSON array of
  `{library_id, komga_root, local_root}`. `KOMGA_READONLY_LIBRARY_IDS` adds
  browse-only IDs. Both default empty. `KOMGA_EDIT_BACKUP_ROOT` is a separate,
  persistent, private root outside mapped libraries and public downloads.
  `SOURCE_SETTINGS_MASTER_KEY` or `SOURCE_SETTINGS_MASTER_KEY_FILE` decrypts
  server-stored Komga credentials. `APP_PUBLIC_ORIGIN` and
  `ALLOW_INSECURE_LOOPBACK` govern admin request protection and local HTTP.
  Never accept a root, path, credential, or backup reference from the browser.
- A book is writable only if its allowlisted mapping exactly matches the
  Komga library root, the library is available and imports book ComicInfo, its
  media is `READY`, and its actual file is a regular `.cbz` inside the mapped
  root. Re-fetch book/library before saving and restoring. Check every path
  component against symlinks and use `os.Root`; a URL prefix alone is not a
  directory boundary. Non-CBZ and unmapped books remain browseable but read-only.
- `Detail` reads the **actual CBZ**, returns its whole-file SHA-256 as
  `source_version`, the current registry, document, per-field `can_set` /
  `can_clear` and reason, `page_count_correction_needed`, and safe block reason.
  `PreviewRequest` carries that SHA, `definitions_version`, at most 32 explicit
  `{key,state,value?}` changes (`value` or `cleared`), and
  `correct_page_count`. An omitted key retains the old XML element. Save adds
  the returned `preview_token` (10-minute lifetime, bound to request and Komga
  book/library revision) and a stable 16–128-character `idempotency_key`.
  Duplicate key plus same request returns the existing reconciled operation;
  a different request with that key conflicts. Preview is read-only.
- Only mapped fields with a verifiable **single-book Komga projection and lock
  state** can be set: `title`, `number`, `summary`, `publication_date`, `tags`,
  `identifiers`, `web`, and the supported writer/penciller/inker/colorist/
  letterer/cover_artist/editor/translator `creators.*` roles. Series-scope and unprojected fields,
  including custom/internal-only fields, remain read-only even if ComicInfo
  has an element. Clear is limited to `summary`, `publication_date`, `tags`,
  `web`, `creators.*`, and `identifiers` only when barcode ISBN import is off;
  title/number cannot be cleared. No implicit unlock or series edit.
  `page_count` is derived, never an ordinary change: if its old XML value
  differs from the actual image count, saving requires a separate
  `correct_page_count=true` and an explicit preview diff. An already-correct
  count cannot be changed by that flag.
- `cbzedit.Inspect` rejects competing/multiple or non-root ComicInfo, unsafe
  XML/Pages, unsupported ZIP structure, bad CRC, path/symlink and size limits.
  Editing uses `comicinfo.EditExisting` plus an allowlist of changed elements,
  not the new-artifact `comicinfo.Merge` policy. Only one exact root
  `ComicInfo.xml` is replaced or appended. Copy other compressed members in
  central-directory order, retain global comment, and read back their names,
  order, headers, compressed and expanded hashes, CRC and page identity.
  Standard `xmlns:xsi` and `xmlns:xsd` declarations with their exact standard
  URIs may be dropped by canonical serialization; other unknown attributes,
  extensions or unpreservable values must block the edit.
- Persist `preparing` before touching files. `Prepare` independently copies
  the complete original into an operation-scoped private backup (not a hard
  link), verifies size/SHA-256 and durable manifests, then creates and checks
  a hidden same-directory `.tmp` archive. Directories/files are `0700`/`0600`.
  Persist `Prepared` hashes and paths **before** `Commit` renames; verify the
  backup, original SHA, temp archive and permissions again. Rename atomically,
  fsync parent, then read back. No auto-expiry or automatic rollback of a
  committed file. A no-XML-difference save still records an idempotent
  operation; it performs no backup/rewrite and may still need Komga sync.
- The operation reports `file_committed` or `file_no_change`,
  `projection_consistent`, and `analyze_verified` separately. After a file
  commit, analyze one book; Komga's `202` and `READY` mean no import proof.
  Apply only whitelisted book-level PATCH clears when ComicInfo import leaves
  stale values, then GET and compare normalized book fields. A clear-only
  match can establish current-value consistency without proving this analyze
  changed anything. On timeout/failure retain the file and backup, mark
  `sync_pending`/`sync_failed`, and retry sync by operation ID only after
  rechecking book path, locks and target SHA.
- Filesystem and PostgreSQL are not one transaction. Startup (bounded to 100),
  operation reads and duplicate saves reconcile private manifests plus current
  old/new SHA before resuming; ambiguity becomes `restore_needed`, never a
  blind second rewrite. Explicit restore re-resolves the allowed book, demands
  the exact current new SHA and verified original backup, and first preserves
  the current edited archive. A later external revision must not be replaced.
  The advisory lock serializes cooperating API instances; last SHA check and
  rename are **not** an atomic compare-and-swap against external writers.
  Do not claim external-writer race freedom without a filesystem primitive.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Anonymous/expired session; invalid Origin or CSRF on mutation | HTTP 401; HTTP 403 respectively, before business work |
| Invalid mapping/env, unsafe or uncreatable private backup root, or missing key | Startup fails closed; no file edit |
| Unknown/disallowed book; non-CBZ/unmapped/import-off/locked/not-READY | 404 for hidden book, otherwise read-only reason or refused preview; no file edit |
| Bad field/state/value, forged series/custom edit, invalid idempotency key | `ErrInvalid` / HTTP `invalid_input`; no file edit |
| Stale SHA/definitions/token/Komga revision or reused key with different request | `ErrConflict` / HTTP 409 `edit_conflict`; no file edit |
| Old PageCount disagrees with image count without explicit correction | Refuse preview/save; do not silently normalize the XML |
| Unsafe ZIP/XML, bad CRC/Pages, metadata competitor, symlink or limit | `cbzedit.ErrInvalidArchive` / `ErrInvalidXML` / `ErrLimit`; refuse before replace |
| Backup digest/permissions/space failure or changed source/temp | `cbzedit.ErrBackup` / `ErrLimit` / `ErrConflict`; retain original and evidence |
| Rename succeeded, later fsync/readback/DB persistence failed | Keep `FileCommitted` evidence; reconcile by hashes, never repeat rename blindly |
| Komga analyze/PATCH/readback failed or timed out | Retain CBZ and backup; `sync_pending`/`sync_failed`, with retry limited to sync |
| Restore target no longer equals this operation's new SHA | Conflict/`restore_needed`; never overwrite later work |

## 5. Good / Base / Bad Cases

- Good: change one book's Summary and explicitly clear Tags. Preview lists
  both; save independently backs up the original, changes only ComicInfo,
  then analyzes and, if needed, PATCH-clears stale Komga Tags. The response
  distinguishes file commit, current-value match and analyze proof.
- Base: an eligible CBZ has no ComicInfo; append one root `ComicInfo.xml`
  without moving or re-encoding images. If XML already matches but Komga has
  an old value, create a no-file-change operation and sync without a backup.
- Bad: duplicate ComicInfo, an external change after preview, a forged series
  field, or an unconfirmed PageCount correction must never rewrite the book.
  A later external file version cannot be overwritten by restore.

## 6. Tests Required

| Tests | Assertions |
| --- | --- |
| `internal/archive/cbzedit/{rewrite,recovery}_test.go` | Independent backup, member bytes/order/headers/comments and original permissions; CRC, duplicate/unsafe XML/ZIP and symlink refusal; PageCount confirmation; crash-window probe and exact-hash restore |
| `internal/app/komgaedit/{catalog,connection,edit_service}_test.go` | Allowlisted path and credential isolation; editable-field/clear matrix; stale token and source version; same-key retry without second write; rename-before-DB recovery; distinct file/projection/analyze results and bounded sync retry |
| `internal/store/postgres/komgaedit/operation_test.go` | Migration-backed idempotency, active-path uniqueness, operation version CAS and cross-instance advisory lock |
| `internal/httpapi/komga_edit_test.go` and admin-auth tests | Status payload does not equate file write with Komga sync; administrator/CSRF/Origin gates; no credential, absolute path, backup reference or XML-body leak |
| Isolated Komga and XML schema checks | Analyze/clear/readback on an isolated book; `xmllint --nonet` against the pinned ComicInfo XSD; never use production CBZs for integration tests |

Run `go test ./... -count=1`; for repository, lock or lifecycle changes also
run `go test -race ./... -count=1` with isolated PostgreSQL test state.

## 7. Wrong vs Correct

Wrong: `comicinfo.Merge` an existing book and call Komga PATCH, then report
success from HTTP 202 or `READY`; this can alter unselected PageCount/Pages and
cannot prove the file or current projection is correct.

Correct: compile only explicit changes with `comicinfo.EditExisting`, inspect
and verify the entire archive, copy a durable private original, persist the
prepared hashes, then commit under the per-path advisory lock. Report the
file result and Komga's normalized readback independently; retry only the
missing stage through the same operation ID.

# ComicInfo packaging and source retention

## 1. Scope / Trigger

Use this contract when changing archive extraction, ComicInfo mapping, derived
metadata, CBZ publication, retention storage or worker source cleanup. It extends
[Task Core runtime](./task-runtime-contract.md) and
[metadata documents](./workspace-metadata-auth.md). The XML protocol belongs in
`internal/comicinfo`, archive IO in `internal/archive`, packaging in
`internal/downloader`; the worker coordinates them without redefining clear,
lock or task-state rules.

This is the implemented contract. In particular, publication uses an atomic
no-clobber hard link; the earlier task design's plain rename wording must not
be used to introduce replacement of existing artifacts.

## 2. Signatures

```go
// internal/archive
func (e *Extractor) ExtractWithMetadata(ctx context.Context, sourcePath string) (*MetadataBundle, error)
func RetainMetadataBundle(ctx context.Context, bundle *MetadataBundle, privateDir string) (RetentionManifest, error)
func RetainEffectiveMetadata(ctx context.Context, privateDir string, raw []byte, manifest RetentionManifest) (RetentionManifest, error)

// internal/comicinfo
func Parse(data []byte) (Parsed, error)
func Merge(raw []byte, submitted metadata.Document, registry metadata.Registry, pageMapping []int, pageCount int) (Result, error)

// internal/downloader
func PackageMetadataCBZ(ctx context.Context, input MetadataPackInput) (MetadataPackResult, error)
func MetadataLocalImages(images []domain.DownloadedImage) []LocalImage

// internal/domain/taskcore; archive aliases these manifest types
func ValidateRetentionManifest(m RetentionManifest) error

// internal/store/postgres/taskcore
func (s *Store) RecordRetention(ctx context.Context, task app.Task, manifest domain.RetentionManifest) error

// internal/worker/taskcore
func (d *TaskDownloader) ExecuteWithMetadata(ctx context.Context, task app.Task) (ExecutionOutput, error)
func (d *TaskDownloader) CleanupSource(ctx context.Context, task app.Task) error
```

`MetadataPackInput` carries `Images`, optional `Bundle`, immutable submitted
`Document`, its versioned `Registry`, `OutputPath` and `PrivateDir`.
`MetadataPackResult` returns `EffectiveDocument`, `Profile`, `Warnings`,
`PreservedStandard` and `RetentionManifest`. These remain separate from input.

Migration [020](../../../internal/store/postgres/migrations/020_task_source_retention.sql)
adds `task_core_results.retention_manifest`;
[021](../../../internal/store/postgres/migrations/021_task_retention_records.sql)
adds `task_core_retention(task_id, generation, manifest, created_at)` with primary
key `(task_id, generation)`;
[022](../../../internal/store/postgres/migrations/022_packaging_report.sql)
adds result `metadata_warnings` and `metadata_profile`. Successful results also
store the effective document and generation. Retention records exist independently
of success, including failed/canceled generations.

## 3. Contracts

### Fixed schema and mapping

The only output profile is
`comicinfo-2.1-draft@99e1453a163c777b4b5320a68732f6f133ac7918`.
Use the vendored [schemas and license](../../../internal/comicinfo/testdata/schema/README.md).
Pinned SHA256 values are:

| Fixture | SHA256 |
| --- | --- |
| `v2.1-draft/ComicInfo.xsd` | `c7a925b75a5297ec66b19898f59597077326a6b812d5ce77b3035fc179d75ed9` |
| `v2.0/ComicInfo.xsd` | `3d9109effff705014f5f6076d92b0ad8c8dad90d993f2592d744c10bad45311c` |

The single [profile table](../../../internal/comicinfo/profile.go) owns the full
XSD element sequence, scalar types and enums. Tests compare the entire sequence
with the pinned XSD. Do not substitute Go struct order. Version 2.0 is an offline
comparison: it rejects Tags/Translator/GTIN extensions; there is no user-facing
2.0 export or silent extension stripping.

| Document field | XML / rule |
| --- | --- |
| `title`, `series`, `number` | Title, Series, Number; Number is a string, including fractional/special issue labels |
| `count`, `volume` | Count, Volume; distinct nonnegative integer concepts, never inferred from a provider's totalVolumes |
| `summary`, `publisher`, `imprint`, `web`, `format`, `age_rating` | Corresponding standard fields; Web must meet canonical public URL validation, AgeRating uses the profile enum |
| `publication_date` | Year plus optional Month/Day; preserve supplied precision |
| `creators.writer/penciller/inker/colorist/letterer/cover_artist/editor/translator` | Writer/Penciller/Inker/Colorist/Letterer/CoverArtist/Editor/Translator respectively |
| `genres`, `tags` | Genre, Tags; comma lists; if any submitted item contains a comma, omit that XML field and emit `list_separator_loss`, retaining the complete internal value |
| `language` | LanguageISO after document validation; do not guess a language from labels |
| `manga`, `reading_direction` | Explicit yes → Yes; yes + rtl → YesAndRightToLeft; no → No; no + rtl warns and omits Manga; unknown/cleared manga cannot imply yes from rtl |
| `identifiers` | GTIN only when exactly one distinct valid export value remains; verify checksum and scheme length/prefix for ISBN/ISBN10/ISBN13/GTIN/EAN; provider record IDs never qualify |
| `page_count` | XML PageCount always uses actual packaged images, regardless of submitted/source count |
| `aliases`, `custom.user.*` | Internal/private sidecar only, with `internal_only` warning; never fabricate XML extensions |

Standard fields outside the editable registry, such as AlternateSeries, Notes,
BlackAndWhite, Characters, StoryArc and CommunityRating, retain valid original
values and appear in `PreservedStandard`. Dates, Manga, GTIN and Pages have special
mapping and are not reported as unmapped preserved fields. Valid decimal lexical
forms such as `4.500` survive; rating validation uses exact arithmetic.

### Merge and strict output

- Validate against the task's original `DefinitionsVersion`; retry must not use
  the current settings registry to reinterpret an old snapshot.
- Absent document key retains the source XML value. `state: "value"` replaces
  that mapped field; `state: "cleared"` removes it. Clear tombstones must never
  be filled back from archive baseline. Clearing `publication_date` removes all
  three date elements; clearing identifiers removes GTIN.
- Fill eligible absent internal fields through `metadata.ApplyCandidate`, with
  archive provenance and the actual XML `SourceField`. Preserve field revisions
  and manual locks. Actual page count is a derived candidate; the XML always
  uses the actual count even if the document's lock/clear policy prevents an
  internal candidate update. Never mutate the submitted document in place.
- Preserve supported untouched standard XML values even when their import into
  the stricter document model is unavailable, with a warning when appropriate.
  Unsafe Web and invalid identifiers must not be re-exported.
- Unknown elements, attributes, comments and competing formats remain in private
  original bytes with warnings. The output contains only supported schema fields.
  XML readback is a runtime profile check, not full XSD validation. Actual XSD
  validation runs in tests using `xmllint --nonet --noout --schema`; missing
  `xmllint` fails that test. CI installs `libxml2-utils`; runtime does not need it.
- Output has one UTF-8 root `ComicInfo.xml`, followed by numbered image entries
  such as `0001.jpg`. No source provenance document, OCR text, credentials,
  competing metadata or arbitrary attachment is added to the CBZ.

### Bounded extraction and page identity

ZIP/CBZ use the ZIP reader; RAR/7Z use streamed `bsdtar`, without extracting
arbitrary archive paths onto disk. `ExtractWithMetadata` returns a partial bundle
on error; callers must retain its source instead of treating failure as empty
metadata. Recognized metadata is bounded to 256 entries, 1 MiB per entry and
8 MiB total including the ZIP comment. Archive limits remain 300 images,
25 MiB/image and 500 MiB across regular members. XML is bounded to 1 MiB and
depth 64; UTF-8 BOM is accepted, DTD/directives/external entities, duplicate XML
attributes, duplicate standard scalar elements and multiple roots are rejected.

ComicInfo recognition uses case-insensitive basename, including nested entries.
A single non-root entry is normalized with a warning. Multiple candidates,
including identical-name duplicate entries, fail with `ErrMultipleComicInfo`.
Recognized competitors include MetronInfo.xml, ComicBookInfo.json, ComicInfo.json,
CoMet.xml and metadata.json. Plain nonmetadata ZIP comments may be retained;
JSON/XML-looking, binary or recognized metadata comments stay private.

`PageIdentity` is `(EntryName, EntryIndex, SourceIndex, OutputIndex)`, not basename.
Stable natural sorting preserves distinct same-name entries and image bytes.
`PageOrderKnown` is true only when source enumeration already agrees with natural
sorting and normalized names are unique. Only then pass a source-to-output
mapping to Merge. Unknown mapping, duplicate/out-of-range page indices or other
ambiguity must produce `pages_unmapped` and omit unreliable Pages data while
retaining the original. Never infer a source reader's page order.

### Reserved paths and durable retention

| Path | Contract |
| --- | --- |
| `DOWNLOAD_PATH/<taskID>/<generation>/<displayName>.cbz` | Public artifact; only the completed result is downloadable |
| `SOURCE_RETENTION_PATH/<taskID>/<generation>/` | Private retained evidence; default root `/app/source-retention`, mounted separately from the public download root |
| `source-archive.bin` | Complete source archive, bounded to 500 MiB; required before releasing the temporary source |
| `metadata-001.bin`, `metadata-002.bin`, … | Individual original metadata bytes; source entry name is manifest information only |
| `zip-comment.bin` | Original ZIP comment bytes |
| `source-manifest.json` | Source-only retention record |
| `effective-metadata.json` | Derived document, profile, warnings and preserved-standard list |
| `metadata-manifest.json` | Final manifest including the effective sidecar |

These are reserved application-generated names. Never use an archive entry name,
task display name or provider URL as a retention write target. Source metadata
must not be served through comic download/copy routes or embedded in the new CBZ.
Deployment must keep `SOURCE_RETENTION_PATH` outside `DOWNLOAD_PATH`; changing the
environment must preserve that separation, not just rely on a dot-directory.

Create private directories with 0700 and files with 0600; reject a symlink source,
nonregular retained file or final directory with group/other permissions. Use
`os.Root`, fixed relative names, source identity checks, bounded copies, file
fsync, size/SHA256 readback and directory fsync. Atomic hard-link retention accepts
an existing file only when bytes/size/hash match; conflicting evidence is never
overwritten. Do not garbage-collect retained originals on Complete, failure,
cancellation or retry; explicit administrative lifecycle management is separate.

Manifest v1 has `files: [{name, original_name?, kind, bytes, sha256}]` and
`source_retained`. Validation caps 512 unique files, 0–500 MiB each, lowercase
64-hex SHA256, and names matching `^[a-z][a-z0-9_-]{0,63}\.(bin|json|xml)$`.
Allowed kinds are source_archive/comicinfo/competing_metadata/zip_comment/
custom_metadata. Exactly one `source_archive` named `source-archive.bin` must
exist when `source_retained` is true. Original names are bounded UTF-8 metadata,
not filesystem paths to resolve.

### Fencing, cancellation and source deletion

1. Retain and register source evidence after extraction, including partial-error
   and progress-error paths, before loading the registry or packaging. The worker
   uses `context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)` for this
   source retention and registration, so cancellation cannot suppress evidence
   and cleanup cannot run indefinitely.
2. `RecordRetention` locks the task row and requires status RUNNING or CANCELING,
   matching lease owner, attempt and generation. Stale/terminal writes return
   `app.ErrConflict`. Do not relax these checks because the context is detached.
3. Package retains evidence first, merges metadata, writes the effective private
   sidecar, then writes a same-directory temporary CBZ. After closing/fsync, read
   every member to EOF to validate CRC, unique names, exact entry order/count,
   uncompressed size, SHA256, XML and expected comment. Publish with
   `os.Link(temporary, output)`, then fsync the output directory. Existing targets
   fail without replacement; do not add a copy or plain-rename fallback.
4. Record the final manifest, then pass effective document/profile/warnings and
   manifest to Complete. Application Complete validates the derived document and
   original definitions version. PostgreSQL Complete requires the exact manifest
   already registered for that task/generation and performs the existing terminal
   owner/attempt/generation fence in the same transaction as the result.
5. Downloader error cleanup removes a target only if `publishedByThisExecution`
   became true. A refused existing target must survive. Executor cancellation
   waits for execution exit, removes that execution's uncommitted output, then
   acknowledges cancellation; it never removes retained evidence.
6. **Upload:** call CleanupSource only after Complete succeeds. Re-read the task:
   same generation, SUCCEEDED, upload kind, and (with metadata enabled) a result
   manifest with `SourceRetained=true`. Resolve the upload through ArtifactAccess
   under `TEMP_PATH` before removing it. Failed/canceled upload sources remain.
7. **Telegram:** its downloaded source is execution-temporary. Set `sourceRetained`
   only after source bytes and the fenced database reference both persist; defer
   deleting this temporary file and its empty source directory until execution
   exits, including a later failure/cancel. Do not delete it immediately after
   initial retention: PackageMetadataCBZ still reads it. If retention/registration
   fails, leave the temporary source. The durable private original remains.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Malformed/oversized/deep XML, DTD or duplicate scalar | `comicinfo.ErrInvalidXML`; preserve source, no CBZ |
| Multiple ComicInfo candidates | `archive.ErrMultipleComicInfo`; preserve source and collected candidates; repair source and submit anew |
| Metadata entry/count/total limit | `archive.ErrMetadataLimit`; return partial bundle for retention |
| Unsupported XML extension/value or ambiguous Pages | Warning, original private bytes retained, unsupported output omitted |
| Comma-containing list or nonunique valid identifiers | Warning; no invented list split/GTIN choice |
| Private copy, permission, digest or registration failure | Stop publication; do not release source |
| Stale owner/attempt/generation, terminal retention write, unregistered Complete manifest | `app.ErrConflict`; no result/state overwrite |
| Existing CBZ destination | Refuse replacement; keep old file intact |
| Write/close/CRC/hash/profile validation error or cancellation | No successful publication; remove own temporary/new output only |
| Canceled execution while source retention is pending | Independent 60-second retention attempt; executor waits before acknowledging cancel |

## 5. Good / Base / Bad Cases

- Good: a source has Title, Notes and Tags; submission clears Title and changes
  Tags. Output omits Title, preserves Notes, writes new Tags and actual PageCount;
  private evidence still contains the original XML and complete archive.
- Base: image-only ZIP with `1/2/10` filenames generates `0001/0002/0003` entries
  with unchanged page bytes and a valid minimal ComicInfo document.
- Bad: two ComicInfo entries, an unresolved Pages index or pre-existing CBZ must
  not be silently selected, guessed or overwritten. A stale worker cannot record
  retention or publish a successful result for the next generation.

## 6. Tests Required

Keep the assertions below when changing the corresponding path. These links point
to actual tests; schema validation alone does not establish reader compatibility.

| Tests | Assertions |
| --- | --- |
| [comicinfo/schema_test.go](../../../internal/comicinfo/schema_test.go) | `TestPinnedSchemaAndActualXSDValidation`: hashes/full sequence, real 2.1 pass, extended 2.0 rejection and core 2.0 pass; identifier schemes, exact decimal preservation, separator/Manga warnings |
| [comicinfo/merge_test.go](../../../internal/comicinfo/merge_test.go) | `TestPreserveStandardMissingClearAndPageMapping`, `TestXMLBoundsMalformedAndDTD`: absence/clear, preserved fields, mapping, bounds and safe parsing |
| [archive/metadata_bundle_test.go](../../../internal/archive/metadata_bundle_test.go) | Natural order, duplicate entry identity, original bytes/manifests, repeat retention, metadata limits, multiple ComicInfo, corrupt CRC and symlink refusal |
| [downloader/metadata_pack_test.go](../../../internal/downloader/metadata_pack_test.go) | Exact image bytes and strict metadata, private evidence excluded from CBZ; injected XML/write/close/validation/cancel/retention failures keep source/old artifact and leave no temporary CBZ |
| [domain/taskcore/retention_test.go](../../../internal/domain/taskcore/retention_test.go) | Safe names, unique files, hashes, kinds and source identity |
| [worker/taskcore/metadata_integration_test.go](../../../internal/worker/taskcore/metadata_integration_test.go) | Derived result/source retention; malformed XML does not publish; `TestWorkerExistingArtifactRefusalDoesNotDeletePreviousOutput`; `TestWorkerCanceledUploadRetainsOriginalWithBoundedIndependentContext` |
| [worker/taskcore/telegram_integration_test.go](../../../internal/worker/taskcore/telegram_integration_test.go) | `TestTelegramTemporaryArchiveReleasedOnlyAfterDurableRetention`: successful/canceled runs release temporary source only after registration and preserve complete durable bytes |
| [worker/taskcore/downloader_test.go](../../../internal/worker/taskcore/downloader_test.go), [executor_test.go](../../../internal/worker/taskcore/executor_test.go) | Cleanup only after success; stale/canceled artifact cleanup; execution exits before cancel acknowledgment |
| [postgres/taskcore/retention_test.go](../../../internal/store/postgres/taskcore/retention_test.go) | Real isolated PG: stale-generation and terminal retention refusal, unregistered-manifest Complete rejection, successful manifest readback |
| [real-upload.spec.js](../../../tests/e2e/specs/real-upload.spec.js) | Browser upload → PostgreSQL/worker → download, natural order/exact page bytes, extended XML and Komga copy |

For implementation changes run the relevant focused packages, real offline XSD
test, worker race tests and isolated PostgreSQL tests; follow the backend quality
gate for the full release. Record actual Komga/Kavita import versions and supported
fields separately. An XSD pass, archive unit test or Komga copy response alone is
not an actual reader import result.

## 7. Wrong vs Correct

Wrong: merge with `if value == "" { useOriginal() }`, keep unknown XML inside a
supposed strict document, or put the full document into a public CBZ sidecar.
Correct: distinguish absent/value/cleared via the domain model, emit only mapped
profile fields and preserve original/private data outside the download root.

Wrong: detach cancellation with an unbounded background context, skip generation
checks for retention, or delete Telegram's source immediately after copying it.
Correct: use the bounded context below for both source copy and fenced registration,
then defer temporary Telegram cleanup until all packaging reads have finished.

```go
retainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
defer cancel()
manifest, err := archive.RetainMetadataBundle(retainCtx, bundle, privateDir)
if err != nil {
    return err
}
return store.RecordRetention(retainCtx, claimedTask, manifest)
```

Wrong: replace a target with `os.Rename`, then run unconditional `os.Remove(path)`
on any worker error. Correct: validate a same-directory temporary file, publish
with no-clobber `os.Link`, and remove only a target this execution actually created.

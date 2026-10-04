# ComicInfo module verification — 2026-10-05

The implementation is complete at the module/worker fixture level. Root owns
full release gates and isolated Komga/Kavita imports. Those reader imports are
not claimed by this report.

## Implemented contracts

- `archive.(*Extractor).ExtractWithMetadata(ctx, sourcePath)` accepts ZIP/CBZ
  and the existing streamed RAR/7Z path. It returns the partial bundle on failure
  so a malformed original can still be retained. Metadata is capped at 1 MiB
  per entry, 8 MiB total (including ZIP comment), and 256 recognized entries;
  existing image/count/aggregate limits remain enforced.
- `archive.RetainMetadataBundle(ctx, bundle, privateDir)` copies the complete
  source to `source-archive.bin`, preserves original recognized metadata and
  ZIP comment bytes, verifies SHA-256/size and fsyncs owner-only files/directories.
  Fixed relative filenames prevent original entry paths from becoming write
  targets. A repeated write accepts only identical bytes; conflicting evidence
  is never replaced.
- `domain/taskcore.RetentionManifest` is the persistence contract; archive uses
  aliases. Validation bounds names, counts, sizes, hash strings and source-file
  identity. Root binds the manifest to task/generation beneath a private root.
- `comicinfo.Merge(raw, document, registry, sourceToOutputMapping, pageCount)`
  generates only the pinned ComicInfo 2.1 draft profile. Missing fields preserve
  source values; explicit clears remove mapped elements; aliases/custom fields
  remain internal. Standard fields without editor mappings remain in XML.
- `downloader.PackageMetadataCBZ(ctx, MetadataPackInput)` retains evidence
  before parsing/merging, writes a same-directory temporary archive, then reads
  every member to EOF (CRC), checks count/order/names/size/SHA-256/XML, and
  publishes atomically without replacing an existing path. The final hard-link
  publication provides no-clobber behavior beyond plain rename. Private evidence
  uses a separate directory and is not a CBZ entry.
- The result contains `EffectiveDocument`, `Profile`, `Warnings`,
  `PreservedStandard` and `RetentionManifest`; submitted fields/locks are not
  mutated. Effective metadata and provenance also have a private JSON sidecar.

## Fidelity and explicit limits

The pinned schema and MIT license are in `internal/comicinfo/testdata/schema/`.
Their SHA-256 values match the approved research. The serializer table is tested
against the complete XSD sequence. XML DTD/directives/external entities are
refused; UTF-8, BOM, 64-level depth, duplicate attributes/elements and bounded
input are checked. Invalid/multiple original ComicInfo files stop publication.
Unknown extensions, attributes, comments, competing metadata and arbitrary
non-image attachments are recoverable from private evidence/full original.

Image identities include original entry path and entry position. Stable natural
ordering preserves duplicate-path bytes and emits `0001.ext`, `0002.ext`, etc.
When original enumeration and natural order disagree, or original names repeat,
the meaning of prior Pages/Image indices is not provable; original Pages are
retained privately and omitted with a warning. A supplied reliable mapping is
validated and applied; duplicate/out-of-range indices are not guessed.

Count/Volume/Number remain independent. Actual PageCount is derived from images.
GTIN accepts only an unambiguous checksum-valid identifier with compatible
ISBN/GTIN/EAN scheme and length. Comma-containing list items are retained
internally and warned instead of silently split. Unknown Manga or contradictory
non-manga/RTL cannot silently manufacture YesAndRightToLeft. Provider provenance,
source configuration, private URLs and custom field JSON are not added to XML.

## Checks actually passed

```sh
go test ./internal/comicinfo ./internal/archive ./internal/downloader ./internal/domain/taskcore -count=1
go test -race ./internal/comicinfo ./internal/archive ./internal/downloader ./internal/domain/taskcore ./internal/worker/taskcore -count=1
go vet ./internal/comicinfo ./internal/archive ./internal/downloader ./internal/domain/taskcore
```

`TestPinnedSchemaAndActualXSDValidation` invokes real local `xmllint --nonet`:
the generated extended output passes pinned draft 2.1, fails pinned 2.0, and a
generated core-only comparison passes 2.0. No online schemas or runtime XSD
process are used. Ubuntu CI needs `libxml2-utils`; root owns that shared CI edit.

Fixtures cover Chinese/XML escaping, absent/set/clear, unedited standard fields,
roles/identifiers/classification, unknown XML, pages ambiguity, private custom
values, same-name pages across/within directories, 1/2/10 ordering, exact image
bytes, corrupt ZIP and image CRC, metadata limits, private source/destination
symlink refusal, and injected write/close/validation/cancellation failures.
Failures preserve the original and previous output and remove temporary CBZs.

Root worker integration was reviewed. Follow-up fixes protect existing outputs
from deferred cleanup after a no-clobber refusal and give retention an independent
bounded context after cancellation. Dedicated upload regression tests in
`internal/worker/taskcore/metadata_integration_test.go` verify both behaviors,
including the finite retention deadline and preserved source digest.

## Remaining integration responsibilities

Root persists success/failure retention references with generation fencing,
effective documents, warnings/profile, and controls later explicit deletion.
Full PG/release/browser gates and actual isolated Komga/Kavita consumption are
separate proof obligations. Recovery files do not imply that arbitrary unknown
attachments remain inside the regenerated CBZ, or that it is a byte-identical
in-place rewrite of the source archive. No production source was modified.

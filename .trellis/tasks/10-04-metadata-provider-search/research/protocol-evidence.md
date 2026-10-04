# Provider protocol verification — 2026-10-05

Batch 2 implementation evidence. All external calls were anonymous, read-only,
used the neutral keyword `Naruto` or its fixed ordinary publication records,
and sent no workspace content or stored credentials. No covers or chapters were
fetched. Synthetic fixtures retain protocol shapes but replace content.

## Current official contracts

The following official documents returned HTTP 200:

| Document | SHA-256 of observed response |
| --- | --- |
| https://mangabaka.org/data/api | `88838383b2fd23f96cdc5deaae46834b79d8b15f7f077e15d062a26f901ddfd4` |
| https://mangabaka.org/about/data-license | `5cc612b45e30ee8b15dc00ae4afc7f6f152eabc0dadc15774ee2073960dbd829` |
| https://api.mangaupdates.com/openapi.yaml | `ef2a347efc32ff47aec19a2795a7dbd89b6e689ed7b6ee3b80ccdd632d836d4c` |
| https://raw.githubusercontent.com/bangumi/api/master/open-api/v0.yaml | `5a7ddb7ddec132293b1aa08102e6ac63e31b2925574658e927d6f931df2519da` |
| https://raw.githubusercontent.com/bangumi/api/master/docs-raw/user%20agent.md | `e5db3dbb0849fe62d4ac51f0a902dc3b230eb25f2a9cb2101ab94a94366d9609` |

MangaBaka documents uncached search 30/min and general reads 180/min.
The implementation spaces these requests by two seconds and one third second,
respectively. MangaUpdates uses a conservative one-second interval; this is an
application policy, not a claimed official numeric quota. Bangumi uses the same
conservative interval and an application/developer/version/project User-Agent.
Every provider respects a bounded Retry-After cooldown without automatic retries.

## Actual implementation smoke

Executed:

```sh
METADATA_PROVIDER_SMOKE=1 go test ./internal/infra/metadataproviders -run TestPublicNeutralSmoke -count=1 -v
```

All three passed, using the actual Go adapter and validating the normalized
detail fields with `metadata.ValidateCandidate(StandardRegistry())`:

| Provider | Search endpoint | Results kept | Detail | Validated fields |
| --- | --- | --- | --- | --- |
| MangaBaka | GET `/v1/series/search?q=Naruto` | 10 | GET `/v1/series/270` | 12 |
| MangaUpdates | POST `/v1/series/search` | 20 | GET `/v1/series/17360452316` | 11 |
| Bangumi | POST `/v0/search/subjects?limit=20`, book type 1 | 20 | GET `/v0/subjects/26595`, `/persons`, `/subjects` | 11 |

MangaUpdates still returned 25 results for an earlier `perpage=2` probe. The Go
adapter independently retains at most 20. Bangumi returned a single-volume
record with a separate `系列` relationship; that relationship becomes `series`,
never `number`. Series volume counts are only mapped to `count` for an explicitly
series record. MangaBaka `final_volume` is not adopted as a current number.

MangaBaka `source` subtrees are deliberately not copied. Only the unified
allowlisted record fields and its own identifier are used, retaining the
MangaBaka attribution and noncommercial/share-alike terms. Nested third-party
fields are not relabeled or implicitly exported. Other sources retain their
own attribution/terms links. The shared provenance now supports `retrieved_at`,
`source_field`, `attribution`, and `license_url`; adoption adds `adopted_at`.

## Deterministic checks passed

- `go test -race ./internal/app/metadatasearch ./internal/infra/metadataproviders ./internal/domain/metadata ./internal/httpapi -count=1`
- `go vet ./internal/app/metadatasearch ./internal/infra/metadataproviders ./internal/httpapi`
- `node --test frontend/src/tests/metadata_search.test.mjs` — 4 tests passed.
- `npx eslint frontend/src/metadata-search/index.js`
- `python3 .trellis/scripts/task.py validate .trellis/tasks/10-04-metadata-provider-search`
- `git diff --check`

Fixtures cover selected/enabled-source isolation; success alongside rate-limit
and timeout errors; explicit registered custom-field mapping; extended standard
fields and provenance through ApplyCandidate/JSON/Validate; config/definitions
changes; fixed HTTPS hosts; Bangumi-only bearer forwarding; redirects; oversized
and malformed responses; Retry-After; finite cache capacity and configuration
separation; per-source one-in-flight limits; cancellation; strict HTTP decoding;
and late/remounted frontend requests preserving manual edits.

## Integration and unverified scope

Root owns router/auth/CSRF assembly, application mounting, rebuilt bundles and
full browser/release gates. This package adds no migration or persistent cache.
The frozen resolve request now explicitly carries schema/definitions/config
versions and optional `custom_mappings`; only registered, enabled,
provider-extractable custom fields with compatible types are accepted.

No real bearer credentials, adult permissions, private collection examples, or
rare-title/adult/doujin coverage have been verified. Anonymous empty results
remain `no_results` with unknown/permission-dependent visibility, and do not
prove absence or filtering. Public smoke is opt-in, so normal tests require no
network access. Third-party API/schema/permission changes remain a runtime risk.

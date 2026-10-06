# Local OCR and guided candidate review

## 1. Scope / Trigger

Read when changing `frontend/src/ocr`, `metadata-search`, shared metadata editor,
workspace shell, extraction-rule settings, OCR resources or their home callbacks.
Use [workspace lifecycle](workspace-lifecycle.md) for administrator/htmx/BFCache
cleanup and [backend extraction protocol](../backend/extraction-protocol.md) for
request types, saved versions and finite-rule parity.

## 2. Signatures

```js
mountOCR(root, {
  draft, api, schema, onInputChange, setConfigRevision,
  onCandidates, // ([candidate], { beforeApply }) or ([]) when invalidated
  recognizerFactory, queueOptions,
}) // { queue, reloadSettings, isDirty, prepare, getInputRevision, markClean, cancelPreparation, dispose }

ocr.prepare({ allowAI: true, signal, retry: false }) // { entries, warnings }
search.prepare({ title, aliases, writers, signal, retry: false }) // { entries, warnings }
editor.prepareCandidates(entries, { signal }) // { appliedKeys, unresolvedCount, warnings }
shell.preflightPreparedCandidates({ signal }) // final confirmation authority gate

editor.showCandidate(candidate, { beforeApply })
shell.showCandidate(candidate, { beforeApply }) // forwards the same options
// beforeApply({signal}) resolves on success; rejection or false refuses adoption.
// showCandidate(null) clears comparison and cancels pending adoption.

createMetadataSearchModule({ root, doc, api, getDraft, getRegistry,
  showCandidate, onInputChange }) // { mount, unmount }
```

Use the injected shared `api.getJson/postJson` transport, returning
`{response,payload}`; pass AbortSignal and use no-store for authority checks.
No candidate or schema transport bypasses the protected session/CSRF layer.

## 3. Contracts

### Image/resource boundary and transient text

Accept only static PNG/JPEG/WebP through one paste/multifile queue. Limits from
`ocr/images.js`: 10 images, 10 MiB each, 50 MiB total, 12,000,000 pixels/image,
8192px/edge. Validate bounded container/MIME/extension/animation/size **before**
pixel decoding, then verify decoded dimensions and close ImageBitmap. Reject the
individual bad image while retaining previously accepted items; no silent resizing.
Browser recognition may use a temporary same-size contrast input from
`ocr/preprocess.js`: short edge >=1000px, >=85% pixels <=80 gray, 0.2–15%
pixels >=160 gray, and fully opaque. Only then map gray >110 to black and
remaining pixels to white. Preserve the original File and preview. Smaller,
light, translucent or ambiguous images stay unchanged; decoded dimensions
must match the bounded header and every bitmap/canvas must be released. This
is a measured browser-only enhancement, not a general accuracy guarantee or
CLI decoder dependency. Cancellation before preprocessing ends must never
start/revive the worker.
Only the dedicated image paste area prevents a paste event containing image files;
ordinary text controls keep native paste behavior. Screenshots never enter archive upload.

One native Worker runs serial recognition. Own it immediately, including resource
initialization, so cancel/unmount really terminates it. Use only same-origin
`/static/ocr/tesseract-7.0.0` worker/core/language paths: Tesseract/core 7.0.0,
`@tesseract.js-data/*@1.0.0/4.0.0_best_int` for chi_sim, chi_tra, jpn, eng.
[Manifest](../../../web/static/ocr/manifest.json) owns file hashes/bytes/source/license;
missing language/resources fail explicitly, never CDN/remote fallback.
`cacheMethod:'none'` disables vendor IndexedDB. Only versioned static assets may
use private immutable HTTP caching; the static route remains authenticated.

Keep image raw OCR, image editable text, merged preview, editable AI send snapshot
and metadata document separate in memory. No screenshots/OCR/send text in
localStorage, sessionStorage, IndexedDB, history snapshots, URLs, logs or CBZ.
Guided automatic preparation sends derived text to the saved AI target after the visible
automatic-preparation choice; the entry explains text/source requests and allows opting out.
Manual advanced extraction keeps its explicit send preview. Images remain local.
Provenance carries references/UTF-16 ranges, not original text or screenshots.

Stable image IDs/generations fence callbacks; indices are not identities. Reorder,
remove, retry and edits advance the input revision. Retry keeps edited text until
explicitly accepting new raw OCR. Preserve source paragraphs/order; duplicate and
conflicting values are visible, never silently overwritten or globally deduplicated.
Incomplete images block full extraction until the user explicitly accepts the
completed subset. Cancel retains completed results and edits.

### Rule/AI intent and one draft

Local rules use finite modes/options and the exact Go/JS normalization contract.
Distinct conflicting field values remain separate candidates. No regex/eval or
automatic field registration. Rule previews stay local; saved rules contain no text.

The approved guided flow invokes OCR, finite captions/rules, the saved AI and enabled
book sources from one preparation action. Common six fields are selected by default;
advanced manual send preview remains available. An edited send snapshot survives later
OCR changes and is marked stale; it must be regenerated or deliberately retained.
No automatic retry, provider switch, chunking or truncation. Ordinary next reuses
unchanged successful preparation; explicit retry rereads saved settings/sources and
invalidates caches while preserving image corrections and metadata.

The shared draft is the sole metadata truth/adoption authority. `inputRevision`
comes from the home coordinator; `configRevision` is saved **AI config_version**.
Preserve the other context member when advancing one. Source config_version and
rules_version are distinct sidecar baselines, not replacements for AI context.
`previewCandidate` can return rows with `.conflict`; absence of a thrown exception
is not permission to apply. `applyCandidate` owns atomic selected-field validation,
manual locks, explicit clears, revisions, invalid input and source eligibility.

### Actual adoption preflight

Guided prepare checks authority before filling only empty, unprotected fields with
one evidenced value. Equal values merge visible sources; different values, manual
locks and clear tombstones need a field decision in the shared editor. Unresolved
fields block final submission. Manual advanced comparison still checks authority
on adoption. Capture immutable request-time versions in each closure, never read
a mutable new settings object as the old candidate's baseline. Final confirmation
rechecks authority for actually adopted fields still present; comparison cleanup
must not discard this check, and a refreshed equal candidate refreshes its authority.

| Candidate | Fresh no-store GETs | Required match |
| --- | --- | --- |
| Finite local caption (`origin=ocr`) | `/api/metadata/schema` | Current definitions; source is `local-caption-v1`, not saved rules |
| OCR rule | `/api/metadata/schema`, `/api/settings/extraction-rules` | Schema/definitions and captured rules_version; validate rules against current schema |
| OCR AI | `/api/metadata/schema`, `/api/settings/ai` | Schema/definitions, captured AI config_version; still enabled with model |
| Book provider | `/api/metadata/schema`, `/api/settings/sources` | Schema/definitions, resolve-time provider_id/source config_version; source exists and enabled |

Editor freezes selected keys/manual-lock confirmation, disables selection and
apply while waiting, but keeps “取消核对” available. No duplicate adoption request.
A thrown error, false, bad HTTP/shape, missing/disabled source or version mismatch
leaves the document intact and shows finite Chinese feedback; never display an
upstream error body. On failure abort sibling authority requests and allow manual retry.

After successful preflight call `draft.applyCandidate` again against current local
baselines; input/field changes during the GETs must still fail atomically. Do not
merge directly or replace the whole document. Candidates without preflight (e.g.
explicit history comparison) keep the existing synchronous draft validation path.

Clearing, replacing, cancelling or unmounting invalidates comparison generation and
aborts its controller. Even a transport ignoring AbortSignal cannot apply late success.
Each OCR/provider closure also rejects a disposed/invalidated owner. Do not add a
BroadcastChannel/polling framework to replace this action-specific authority check.
These GETs and local adoption are not a distributed transaction with settings writes.

### Lifecycle and retry

Real unload/logout releases Worker, object URLs, decoded images, listeners, pending
settings/AI/source/adoption requests and in-memory text. Guard DOM and draft use by
mounted/generation/controller identity. Cancelled navigation keeps the current state.

`retryMetadataWorkspace` must preserve any OCR or shell unsaved changes. Clean schema
retry disposes OCR and unmounts search **before** shell.retry disposes the old draft;
a late worker callback must not target a retired draft while schema reload waits/fails.
Home must forward the second candidate callback argument through the shell to editor.

## 4. Validation & Error Matrix

| Condition | UI behavior |
| --- | --- |
| Corrupt/spoofed/animated/oversized screenshot | Explicit error; keep accepted images; no decoder-before-budget |
| Worker/resource failure | Failed image remains retryable; no remote fallback |
| Some images unfinished | Require explicit completed-subset selection |
| OCR changed after send text edited | Keep edited snapshot, mark stale, require renewed confirmation |
| AI/refusal/auth/timeout/context/schema failure | Keep all text and metadata; no automatic retry |
| Unresolved guided field conflict | Keep current draft; final confirmation blocked until keep/replace/manual edit |
| Keyed submission response lost | Read protected submission receipt; retry same snapshot/key, not a new task |
| Preflight error or retired authoritative version | No draft mutation; safe feedback; manual retry/reacquire |
| Selected field/input/config conflict after preflight | Shared draft rejects the whole selection |
| Pending adoption cancelled/replaced/unmounted | Abort + retire generation; ignore late success/error |
| Dirty schema retry | Keep OCR and document; show preservation feedback |

## 5. Good / Base / Bad Cases

- Good: correct two screenshots, explicitly omit a failed third, automatically
  prepare evidenced empty fields, settle only conflicts, and confirm once. Advanced
  manual candidates retain the same adoption/final authority checks.
- Base: AI unavailable; type metadata or use local rules and continue independently.
- Bad: another tab disables a provider while its comparison is open; applying the
  old candidate must fail without clearing manually entered fields or OCR text.

## 6. Tests Required

- [Guided review tests](../../../frontend/src/tests/candidate_review.test.mjs):
  safe prefill, same-value source aggregation, manual clear/locks, stale authority,
  comparison cleanup retaining actual adopted authority and final preflight.
- [Image tests](../../../frontend/src/tests/ocr_images.test.mjs): header/animation/
  bounds before decode, decoded-size mismatch, bitmap closure.
- [Queue tests](../../../frontend/src/tests/ocr_queue.test.mjs): serial recognition,
  generation fencing, cancellation/terminate, edited text across retry, partial consent.
- [OCR module tests](../../../frontend/src/tests/ocr_module.test.mjs): text paste native,
  guided automatic preparation and advanced AI confirmation, send snapshot retention, rule/AI authority GET only on actual
  apply, version/schema/disabled/HTTP/network/malformed rejection and late owner cleanup.
- [Rule tests](../../../frontend/src/tests/ocr_rules.test.mjs) and
  [settings tests](../../../frontend/src/tests/extraction_rules.test.mjs): typed finite
  matching/label parity, preview privacy, CAS conflict keeps form and version.
- [Editor tests](../../../frontend/src/tests/metadata_editor.test.mjs): wait/disable,
  duplicate clicks, safe rejection/sibling abort, replace/clear/cancel/unmount with
  ignored abort, postflight local conflicts and no-preflight adoption.
- [Provider tests](../../../frontend/src/tests/metadata_search.test.mjs): captured source
  version differs from AI context; current schema/source checks and late invalidation.
- [Retry tests](../../../frontend/src/tests/home_metadata_retry.test.mjs): dirty OCR
  preservation and disposing source modules before retiring draft.
- [Resource verifier](../../../scripts/verify_ocr_resources.py) and
  [browser OCR check](../../../scripts/check_ocr_browser.mjs): manifest hashes, real
  four-language WASM, no external requests, missing resources fail, cached assets offline.

Run frontend tests/lint/build and include rebuilt dist artifacts. Production-bundle
E2E must exercise protected workspace navigation/cleanup; a minimal DOM double cannot
prove browser memory, mobile limits, screenshot accuracy or real-model connectivity.
Keep synthetic OCR metrics separate from those live acceptance claims.

## 7. Wrong vs Correct

Wrong: `onCandidates` directly edits fields, compares only local versions at preview,
or assumes `abort()` alone prevents late writes.

Correct:

```js
onCandidates: (items, options) => shell.showCandidate(items[0] || null, options)
// Editor awaits beforeApply({signal}), checks its generation, then calls
// draft.applyCandidate(candidate, frozenKeys, {confirmLocked}).
```

Preserve model/source/rule version identities and rerun shared atomic draft checks
at adoption, after asynchronous authority verification.

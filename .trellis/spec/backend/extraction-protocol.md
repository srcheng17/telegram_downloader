# Saved rules and text extraction protocol

## 1. Scope / Trigger

Read before changing `internal/app/extractionrules`, `internal/app/metadataextract`,
`sourcesettings/extraction.go`, `internal/modelapi`, their HTTP routes or extraction
storage. This supplements [workspace metadata/auth](workspace-metadata-auth.md).
Browser OCR and adoption are in [candidate adoption](../frontend/candidate-adoption.md).

## 2. Signatures

- `GET /api/settings/extraction-rules` → `{rules_version,definitions_version,rules,warnings?}`.
- `PUT /api/settings/extraction-rules` ← `{expected_version,definitions_version,rules}`;
  the returned `rules_version` increments only after a successful CAS write.
- `POST /api/metadata/extract` ← `metadataextract.Input` →
  `{request_id,candidates:MetadataCandidate[],warnings:Warning[]}`. It never saves or adopts.
- `metadataextract.AIClient`: `Snapshot(ctx,expectedConfigVersion)` and
  `ExtractJSON(ctx,ModelSnapshot,prompt,jsonSchema,outputBudget)`.
- `modelapi.Client.ExtractionCapability(ctx,base,key,model)` returns
  `{ContextTokens,Fingerprint}`; `Extract(...,outputBudget,capability)` uses that identity.
- Migration 016 owns the non-secret singleton `extraction_rules`
  (`rules_version`, `definitions_version`, `rules`, `updated_at`). Do not rewrite it
  after deployment; schema changes require a forward migration.

## 3. Contracts

### Saved rules and Go/JavaScript parity

`Rule={id,target_key,labels,mode,label_value_options?}`; modes are exactly
`label_value`, `continuation` (string only), `hashtag_list` (string[] only).
Target fields must be enabled and allow `rule`; `page_count` is not a rule target.
No user regex, JavaScript, expressions, URLs or executable templates.

Rules: at most 128; IDs match `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}` and are unique;
1–8 labels/rule, each ≤128 UTF-8 bytes, nonempty, no control characters or colon.
PUT body and browser serialized rule set are bounded at 64 KiB; Go also bounds
serialized rules. Unknown/repeated JSON members are rejected through `metadata.DecodeJSON`.

Normalize labels for duplicate/ambiguity validation using **Unicode White_Space
trim and ASCII A-Z folding** in Go and JS, regardless of matching options. Do not
substitute Unicode lowercase or JS `.trim()` (dotted I/sigma/BOM differ).
Duplicate normalized labels within one rule, or mapping one normalized label to
different targets, are invalid. Same-target labels across separate rules are allowed.

Options omitted → all four separators and both booleans true. Options supplied
must include all members, reject null, and contain no unknown keys:

```json
{"separators":["comma","chinese_comma","semicolon","newline"],"case_insensitive":true,"trim_space":true}
```

Separators are unique; `[]` is valid. Case-insensitive matching folds English
letters only. Stale rule definitions return a warning on GET and cannot execute
until explicitly repaired/saved. Saving locks `metadata_definition_head FOR SHARE`,
checks its version, then CAS writes the singleton in one transaction. Registry
writers take `FOR UPDATE`; never separate validation from that lock/write boundary.

### User-confirmed extraction, saved configuration and privacy

Input requires `request_id,text,field_keys,schema_version,definitions_version,
base_document_revision,field_revisions,input_revision,config_revision`;
`rules_version` is optional provenance, not executable instructions.
The 128 KiB body accepts valid UTF-8 text ≤64 KiB, 1–64 unique enabled AI-extractable
keys, exactly matching field revision keys, and bounded compatible revisions.
Do not accept client `base_url`, `model_id`, credentials, tools, screenshots or a
whole document. Saved settings supply the exact destination/model/key.

A saved disabled configuration returns `disabled` **before decryption or network**;
missing config/model returns `not_configured`. Config version must match before
and after inference. `ModelSnapshot.Handle` is internal and `json:"-"`; its opaque
containing type must redact String/GoString and refuse JSON encoding. A secret's
String method alone does not protect reflection through unexported containing fields.

One **120-second parent context** covers schema/config probes, template rendering,
tokenization, inference and postflight checks. Child calls cannot extend it;
workspace HTTP write deadline is 130s. Shared model transport has bounded responses
(1 MiB), cancellation and no redirects. No new extraction-specific credential env;
reuse the master-key/auth contracts in workspace metadata/auth.

Only selected definitions produce JSON Schema. Model output is
`{fields:{<key>:{value:<typed>,evidence_quote:<exact input substring>}}}`.
Reject unknown keys, invalid types/bounds, invented/ungrounded values, clears/nulls,
truncated/incomplete results and incompatible definitions/config after inference.
An empty fields object yields no candidate. Quotes locate evidence; provenance
stores UTF-16 start/end offsets and non-secret references, never the quote/OCR body.
Repeated quotes get ambiguity warnings. Persist only explicitly adopted metadata;
no OCR/request/model body in DB, logs, errors, traces or CBZ.

### Exact native llama.cpp budget capability

Current extraction supports the native protocol pinned to commit
`11fe02151f79c41d0d4af7da708755d73b9c0da6`. OpenAI-compatible `/models` or a successful
settings test alone is insufficient. Require a unique selectable exact model;
`/props` must supply matching model alias, nonempty path/template, valid slot n_ctx,
and build `^b[0-9]+-([a-f0-9]{7,40})$` whose commit is a prefix of this pinned commit.
Fingerprint includes context, template, build and model identity; recheck around inference.

1. `/apply-template` renders system prompt + selected-field schema + user text;
   `add_generation_prompt:true`, `chat_template_kwargs.enable_thinking:false`.
2. `/tokenize` uses `add_special:true,parse_special:true`. Reject missing/empty,
   null, negative or non-int32 tokens. Budget the exact returned array.
3. Require `prompt_tokens + outputBudget + 8 <= /props context`. Current application
   `OutputBudget=1024`; do not imply that every long summary fits this reserve.
4. `/completion` receives **that same numeric array**, exact model, json_schema,
   n_predict=outputBudget, temperature=0, stream=false, cache_prompt=false, n_keep=0.
5. Require model match, stop=true, truncated=false, stop_type=eos,
   tokens_evaluated=sent array length, 1≤tokens_predicted≤budget,
   generation_settings.n_predict=budget, generation_settings.stream=false, valid JSON.
   Non-null refusal rejects. Completion **does not contain n_ctx** in this build;
   it belongs to `/props`. See pinned [native response source](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/tools/server/server-task.cpp#L340).

No character-count token claim, silent truncation, automatic chunking, retry, model
switch or context enlargement. Changing supported server versions requires source
and live-protocol verification. Reported build/model identity is not binary or
weights attestation. Empty fields cannot distinguish no evidence from semantic refusal.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Invalid/oversized rules or explicit null options | 400 `invalid_rules`; no write |
| Rules CAS/definition mismatch | 409 `rules_conflict`; keep current values |
| Invalid extraction body/keys/revisions/text | 400 `invalid_request` or `input_too_large` |
| Unsupported native capability/definition/schema | 400 `schema_unsupported`; no speculative inference |
| Exact prompt plus reserve exceeds context | 400 `context_exceeded`; no completion request |
| AI config changed | 409 `config_changed`; no candidate |
| Disabled/missing saved AI config | 503 `disabled`/`not_configured` |
| Rate limit / deadline / user cancellation | 429 `rate_limited` / 504 `timeout` / 499 `cancelled` |
| Upstream refusal/auth/permission/invalid body/unreachable | 502 with finite code; no upstream message |

Native HTTP400 classification reads only a bounded machine-code envelope:
`exceed_context_size_error` → context_exceeded; `invalid_request_error` →
schema_unsupported. Unknown errors stay generic. Extraction responses are no-store;
root admin/CSRF middleware still applies (workspace auth 401/403 is distinct from upstream auth).

## 5. Good / Base / Bad Cases

- Good: selected registered translator/date/custom fields with direct evidence
  produce an unapplied canonical candidate under unchanged saved versions.
- Base: AI disabled; local rules/manual workflow still works; no secret decryption/network.
- Bad: stale config, missing BOS in budget, invented evidence, truncated JSON or
  unsupported generic endpoint; reject the whole result and preserve the user's draft.

## 6. Tests Required

- [modelapi extraction tests](../../../internal/modelapi/extraction_test.go): exact
  tokens and special-token options, overflow before completion, identity/version drift,
  missing/null counts, output bounds, refusal and final native response without n_ctx.
- [modelapi transport tests](../../../internal/modelapi/client_test.go): bounded/redacted
  native errors, cancellation, credentials not forwarded on redirects.
- [saved snapshot tests](../../../internal/app/sourcesettings/extraction_test.go):
  disabled/no-network, config changes, finite errors, JSON and `%+v` omit synthetic secret.
- [extraction service tests](../../../internal/app/metadataextract/service_test.go):
  one deadline, typed/custom fields, UTF-16 evidence, ungrounded/partial output rejection.
- [HTTP tests](../../../internal/httpapi/metadata_extract_test.go): strict input/no-store,
  error status and no body/credential leakage.
- [Go rule tests](../../../internal/app/extractionrules/service_test.go) and
  [JS parity tests](../../../frontend/src/tests/ocr_rules.test.mjs): identical finite labels/options.
- [PG rule tests](../../../internal/store/postgres/extractionrules/store_test.go):
  saved CAS/reopen/definition conflict against an isolated database; a skip is not evidence.

Run focused Go tests/race/vet plus full repository gates from the backend index.
Actual protected product-container → model verification remains a separate gate;
transport mocks and source review cannot establish live reachability or extraction quality.

## 7. Wrong vs Correct

Wrong: trust a README completion n_ctx example, count only raw OCR characters,
log an opaque model handle, or send client-supplied model/destination.

Correct: read slot context from `/props`, tokenize the complete rendered prompt with
special tokens, send that array unchanged, redact the containing handle and resolve
only the saved configuration. Refuse when the protocol cannot prove these invariants.

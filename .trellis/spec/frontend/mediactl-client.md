# `mediactl` Node client contract

## 1. Scope / Trigger

Read before changing `cli/`, its shared pure modules under `frontend/src/`, the
protected API used by the CLI, or `.agents/skills/media-workspace-cli/`. The CLI
is an administrator HTTP client. Its local OCR and draft broker may reuse pure
frontend metadata/rule modules, but task eligibility and persistence remain in
the Go application/Task Core. It must not open PostgreSQL, Komga mounts, or tdl
session files. `mediactl --help` is the command inventory; no Telegram-message
task creation command exists.

## 2. Signatures

```text
mediactl [--server HTTPS_ORIGIN] auth login|password-change
mediactl [--json] auth status|logout
mediactl [--json] workspace start
mediactl [--json] workspace status --workspace ID
mediactl workspace review --workspace ID
mediactl [--json] workspace stop --workspace ID --revision N
mediactl workspace attach --workspace ID --jsonl
mediactl [--json] workspace field set --workspace ID --revision N --field KEY --input-json -
mediactl [--json] workspace tasks create-url --workspace ID --revision N --url URL [--force] [--accept-partial]
mediactl [--json] workspace tasks upload --workspace ID --revision N --file PATH [--accept-partial]
mediactl workspace reveal prepare --workspace ID --revision N --kind merged|image|candidate|field|record [--id ID]
mediactl --json workspace reveal consume --workspace ID --revision N --ticket UUID --to-ai-context
mediactl [--json] komga metadata preview|update --id BOOK_ID --input-json -
mediactl [--json] komga edits status|sync|restore --id OPERATION_ID
```

`--input-file PATH` is the alternative to `--input-json -`; both accept one
structured JSON object of at most 1 MiB and cannot be combined. Text and field
values go through the structured input; the Skill also uses it for untrusted
URLs and paths instead of shell interpolation. Workspace task JSON accepts
`{"url":"…","force":true,"accept_partial":true}` or
`{"file":"…","accept_partial":true}`; a JSON `force:true` also needs an
explicit `--force`, while `accept_partial` must be explicit before unfinished
images can be omitted. `workspace attach --jsonl`
accepts one `{ "op": "field-set",
"expected_revision": 3, "args": { "key": "title", "value": "..." } }`
per line and returns a `seq`-numbered safe result. JSONL does not expose review,
AI extraction, or reveal operations.

## 3. Contracts

- The client accepts an HTTPS Origin only; HTTP requires the explicit
  `--allow-insecure-loopback` flag and a loopback target. It rejects redirects
  instead of forwarding cookies. The administrator session lives in the current
  user's `~/.config/mediactl/session.json` (`0700` directory, `0600` file);
  network commands and broker startup revalidate `/api/auth/session`; the
  broker checks its local session identity while running. Unsafe HTTP requests send exact
  `Origin` and `X-CSRF-Token`; each business write first reads protected
  `GET /api/client-contract` and requires
  `{ "client": "mediactl", "protocol_version": 1 }`. Login/logout remain
  usable without that handshake. Incompatible or missing contracts fail before
  the business mutation.
- Passwords, 2FA and replacement source/AI/Komga credentials are read hidden
  from an independent user TTY. They are not accepted in argv, JSON/JSONL,
  environment variables, piped stdin or `--json`; `review` and AI-send
  confirmation also require that TTY. The AI Skill may direct the user to that
  terminal and later read a safe status, but must not treat an AI-created PTY as
  the independent review channel.
- `workspace start` creates one in-memory broker per work. Its private user
  directory and Unix socket are `0700`/`0600`, its ID is random, and an idle
  broker expires after 30 minutes. A mutation carries the current integer
  `--revision` (JSONL: `expected_revision`); a changed revision returns a
  conflict without silently overwriting the draft. Disconnecting a client does
  not serialize the document; logout, stop, idle expiry, or broker death loses
  it. Explicit `workspace export --output ABSOLUTE_PATH` creates a new `0600`
  document file and excludes OCR text.
- PNG/JPEG/WebP OCR runs on the client with packaged resources: at most ten
  images, 10 MiB each, 50 MiB total, 12 megapixels and 8192 pixels per side.
  Images and OCR text stay local. Review, candidate values, merged text and AI
  send preview appear only through `workspace review` on the independent TTY.
  Default `--json`/JSONL responses expose IDs, keys, revisions, counts and
  warning codes, not content. `workspace ai extract` sends only selected text
  after reviewing target, model, field keys and exact send snapshot in that TTY.
- A user who explicitly asks the AI to interpret one piece of content may use
  `workspace reveal prepare` for one `kind`/`id`, then the exact
  `--json ... reveal consume ... --to-ai-context` form. Prepare returns only a
  ticket, revision and byte count. One ticket holds at most 256 KiB, expires in
  60 seconds, is bound to the workspace revision and is consumed once; any
  intervening broker revision change invalidates it. Consume is the sole intentional
  machine-output exception for selected content. JSONL cannot reveal.
- `workspace tasks create-url|upload` validates the current in-memory document
  against fresh server schema and submits that document through the ordinary
  protected task endpoint. An unfinished image requires explicit
  `--accept-partial`. A duplicate URL requiring confirmation returns
  `needs_confirmation`/`snapshot_attached:false`; it has not stored the draft
  on a new task. Explicit `--force` is needed to re-fetch. A successful upload
  or newly created URL task returns `snapshot_attached:true`, task ID and the
  submitted document revision. Argument parsing leaves an absent `--force` as
  `undefined`, so compare its boolean meaning with JSON `force:false`; do not
  reject an ordinary structured URL request for `undefined !== false`.
- Komga `books edit` returns safe field eligibility and versions. `metadata
  preview` takes `source_version`, `definitions_version`, and `changes`;
  `update` also requires the preview token and idempotency key. The operation
  is read back via `edits status`. Check `file_committed`,
  `projection_consistent`, `analyze_verified`, state and `available_actions`;
  an HTTP success or `verification_pending:true` does not prove Komga indexed
  the edit. `sync`/`restore` require the advertised action. Detailed book,
  diff and connection values use independent-terminal `review` commands.

## 4. Validation & Error Matrix

| Condition | CLI result |
| --- | --- |
| Bad command/JSON/image/value | `invalid_input` (or specific validation code), exit 2; no mutation |
| No/expired session, rejected CSRF or Origin | auth code, exit 3; clear expired local session; no password replay |
| Stale workspace revision, settings version, candidate or Komga source | conflict code, exit 4; preserve local draft/input |
| Missing/incompatible client contract, unavailable broker/service | `incompatible_server` or unavailable code, exit 5; no business write |
| Invalid response, redirect or transport failure | transport code, exit 6; do not echo upstream body; on uncertain Komga save retain the same input/idempotency key, then query a known operation or retry |
| Password, `review`, AI send or QR requested through JSON/no user TTY | `interaction_required`, exit 7; no attempt or secret read |
| Reused/expired/revision-stale reveal ticket | `workspace_conflict`, exit 4; no selected content output |

Success uses `--json` stdout `{ "ok": true, "code": "ok", "data": ... }`;
failure uses stderr `{ "ok": false, "code": "...", "message": "..." }`
and a nonzero exit. Normal machine output and safe errors must not include OCR
text, R18 field values, credentials, session tokens, QR payloads, private error
bodies or host absolute paths. The explicit reveal consume exception applies
only to its selected content.

## 5. Good / Base / Bad Cases

- Good: create a workspace, OCR several screenshots locally, inspect values in
  a separate terminal, adopt selected keys, then submit the same document via
  `workspace tasks upload`; verify task ID and `snapshot_attached:true`.
- Base: without OCR or AI, set a field through structured JSON, validate and
  submit a URL; empty/clear/absent fields retain their distinct meanings.
- Bad: a prepared candidate becomes stale after its source setting changes;
  adoption or Komga save must reject the stale version and retain edits.

## 6. Tests Required

- `cli/tests/http.test.mjs`, `auth.test.mjs`, `session_store.test.mjs`: exact
  protocol handshake before every business write, missing/mismatched version
  rejection, HTTPS/loopback/redirect, Origin/CSRF and private session modes.
- `cli/tests/workspace.test.mjs`, `reveal.test.mjs`: safe stdout/stderr/JSONL
  with synthetic sensitive text; cross-command broker state, concurrent
  revision conflict, OCR resource limits, TTY confirmation and reveal's
  one-use/TTL/revision/size gates. A stopped broker cannot resurrect a draft.
- `cli/tests/tasks.test.mjs`, `komga.test.mjs`, `settings.test.mjs`: direct
  workspace document submission, duplicate confirmation, CAS inputs,
  preview/update/idempotency, operation readback and partial status.
- Run `npm run test:cli`, `npm run lint:cli`, `npm run build:cli`, then `npm
  pack` and clean-install `mediactl --help/--version`. If a shared frontend
  module changes, also run frontend tests/lint/build and include rebuilt
  `web/static/dist/` bundles. Browser E2E and live external-client checks
  remain separate from unit assertions.

## 7. Wrong vs Correct

Wrong: `mediactl workspace field set --value "<OCR text>"`, paste an API key
into JSON, treat a successful Komga save request as fully synchronized, or use
`options.force !== (input.force === true)` when `--force` may be absent.

Correct: send ordinary field values as a JSON object through
`--input-json -`, enter secrets in a separate user TTY, and read back the
Komga operation's file and projection status. Normalize the optional flag with
`Boolean(options.force)` before comparing it with the JSON request. Use
explicit reveal consume only after the user asks the AI to inspect the selected
content.

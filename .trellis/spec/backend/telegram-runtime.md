# Telegram account and download runtime

## 1. Scope / Trigger

Read before changing account login, helper/tdl process control, Telegram Task Core
inputs or their deployment. This is one administrator and one active account.
Real account validation is a separate acceptance gate from controlled tests.

## 2. Signatures

- `GET /api/telegram/account`, `POST /api/telegram/account/verify`.
- `POST /api/telegram/login-attempts`; GET `/{id}` and `/{id}/events`, POST
  `/{id}/password` and `/{id}/cancel` under that prefix.
- `POST /api/telegram/downloads` accepts `{message_url, force, metadata_document}`.
  HTTP obtains `Service.SnapshotForTask`; caller-supplied account identity is invalid.
- `TaskCore.CreateTelegramTask(CreateTelegramInput)` stores kind `telegram` and an
  immutable `{message_url, account_revision, account_identity}` source snapshot.
- Migrations 018/019 add account coordination and task input; 020–022 add packaging
  retention/report contracts. Use forward migrations, never rewrite applied files.

## 3. Contracts

All routes, QR resources and SSE require administrator authentication; mutations
require CSRF. QR/password/session files are never logged or included in task input.
An attempt returns `{attempt_id, seq, revision, state, code?, expires_at,
qr_expires_at?, qr?}`. Consume only the current attempt with increasing sequence;
clear QR/password on expiry, state change, cancellation and unmount. Reconnect reads
the current snapshot and does not replay obsolete QR events.

Only a structured authorization event, successful process exit and a separate
process restoring the candidate session permit CAS account promotion. An exit code
or human-readable `Login successfully` alone does not prove account authorization.
Login, verification and downloads share a PostgreSQL advisory lock and an OS file
lock inherited on fd 3. On cancellation or PostgreSQL connection loss, cancel and
wait for children before releasing the lock. The helper also enforces its private
command deadline when the parent dies.

The helper and official tdl CLI use matching pinned module dependencies (tdl
0.20.4); build the nested module with `-mod=readonly`. Credentials travel only via
private stdin; use namespace allowlists and owner-only nonsymlink directories.
Do not parse CLI terminal output or silently import an existing private session.

The pinned gotd client converts `PASSWORD_HASH_INVALID` into
`auth.ErrPasswordInvalid`. Use one shared predicate with `errors.Is` for direct or
wrapped sentinels and `tgerr.Is` for raw RPC errors in both classification and the
three-attempt password retry loop. `PASSWORD_EMPTY` remains classified as
`password_invalid` without retrying. Never trim or otherwise change the password.

Only one ZIP/RAR/7Z document attachment is accepted. Inspection must establish a
positive declared size below the cap; reject ambiguous filenames/media. Enforce
the output cap in the helper while streaming and verify actual bytes/hash afterward.
Canonical Telegram URLs have bounded int32 message IDs; topic URL variants resolve
to the same message because official tdl ignores the topic segment. Never auto-join,
send messages or change account content restrictions.

`TELEGRAM_ENABLED` defaults false in Go and true in Compose. The strict
`TELEGRAM_MAX_SOURCE_BYTES` range is 1..524288000; the account DTO exposes the effective
`max_source_bytes`. `TELEGRAM_PRIVATE_ROOT`, `TELEGRAM_HELPER_PATH`,
`TELEGRAM_TDL_PATH` and `SOURCE_RETENTION_PATH` configure private runtime storage.
API and worker share session and retention volumes; changed APP_UID requires matching
volume ownership. Never serve either private root as static files.

SSE sends 15s heartbeats and renews a 20s write deadline. The root authentication
middleware rechecks long-lived sessions, and nginx disables SSE buffering with 70s
read/send timeouts. Ordinary Telegram HTTP calls have a bounded 65s write deadline.
Private source cleanup requires a durable copy plus fenced retention registration;
see [packaging](metadata-packaging.md). A failed registration leaves the temporary
source intact. Retry retains the original account snapshot and dedup identity.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Concurrent account operation | 409 `busy` |
| Stale attempt/account identity | 409 `attempt_conflict` / `account_changed` |
| Missing authorization | 409 `auth_required` |
| Bad link/media/unknown or excessive size | 422 finite error code |
| Invalid 2FA password | 422 `password_invalid`, no password echo |
| Telegram rate limit | 429 `rate_limited` |
| Disabled/missing runtime | 503 `telegram_unavailable` |
| Unexpected process/network error | Bounded generic failure; no raw stderr |

## 5. Good / Base / Bad Cases

Good: scan, confirm, restore in a fresh process, then enqueue one bounded attachment.
Base: account unavailable; editor remains usable and Telegram operations fail clearly.
Bad: swap accounts then execute an old task, accept zero/unknown size, or unlock while
a child still holds the session; these must fail closed.

## 6. Tests Required

- `internal/app/telegram`: attempt state/CAS, stale identity, size and URL validation.
- `internal/infra/telegram`: three-process contention, PostgreSQL loss cancellation,
  process reaping, bounded structured events, candidate-only removal.
- `tools/tdl-auth-helper`: hard stream cap, private deadlines, attachment ambiguity,
  direct/wrapped password sentinels and raw RPC classification/retry compatibility;
  Linux amd64/arm64 readonly builds.
- `internal/store/postgres/{telegram,taskcore}`: real PG promotion, immutable input,
  dedup/retry and stale-generation fences.
- `internal/worker/taskcore/telegram_integration_test.go`: source retention before
  cleanup, cancellation/failure paths; no cleanup when retention registration fails.
- HTTP/Node tests: auth, CSRF, event cleanup, late snapshots and password clearing.
- Manual real-account smoke: use `tools/tdl-auth-helper/README.md`; controlled tests
  do not prove QR handoff, channel access or a long-running real nginx SSE session.

## 7. Wrong vs Correct

Wrong: promote on CLI exit, persist QR to browser storage, accept client namespace,
or delete the downloaded archive before its durable retention record exists.
Correct: structured authorization plus fresh restoration, transient protected UI,
server-issued immutable account snapshot, then fenced durable retention and cleanup.

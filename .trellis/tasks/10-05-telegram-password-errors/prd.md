# Telegram two-factor error recognition

An administrator reports that Telegram login terminates after entering the two-factor password. The pinned gotd client converts PASSWORD_HASH_INVALID to auth.ErrPasswordInvalid, while the helper checks only RPC errors. This deterministically misclassifies invalid passwords as network_error and bypasses the existing retry flow. The reported attempt may have another cause; real account verification remains required.

## Requirements

- Recognize direct and wrapped gotd auth.ErrPasswordInvalid as password_invalid.
- Retain raw PASSWORD_HASH_INVALID recognition and the existing three-attempt limit.
- Preserve successful authorization, fresh-process verification, cancellation, locks, private storage, and bounded event protocol.
- Never log, expose, trim, or otherwise change passwords, QR material, or Telegram sessions.
- Keep all pinned dependencies and runtime protocols unchanged.

## Acceptance Criteria

- Regression tests fail before the fix for gotd sentinel errors and pass afterward.
- Nested helper tests, vet, and Linux amd64/arm64 readonly builds pass.
- Root backend tests pass; skipped external database checks are reported accurately.
- Deploy through Dockhand with a backed-up source and previous immutable image for rollback.
- Report real Telegram account login and independent verification separately from controlled checks.

## Change boundary

The behavior lives in tools/tdl-auth-helper/main.go. Change only its password-error recognition, helper regression tests, and the relevant Telegram specification. Do not alter other authentication, frontend, storage, proxy, or AI behavior. Deploy a derivative of the exact running image containing only the rebuilt helper.

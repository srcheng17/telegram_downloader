BEGIN;
CREATE TABLE IF NOT EXISTS telegram_account (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    namespace text NOT NULL DEFAULT '',
    identity text NOT NULL DEFAULT '',
    verified_at timestamptz
);
INSERT INTO telegram_account(singleton) VALUES(true) ON CONFLICT(singleton) DO NOTHING;
CREATE TABLE IF NOT EXISTS telegram_login_attempts (
    id text PRIMARY KEY,
    owner text NOT NULL,
    namespace text NOT NULL UNIQUE,
    expected_revision bigint NOT NULL CHECK (expected_revision >= 0),
    seq bigint NOT NULL CHECK (seq > 0),
    revision bigint NOT NULL CHECK (revision > 0),
    state text NOT NULL CHECK (state IN ('waiting_qr','password_required','verifying','connected','cancelled','expired','failed')),
    code text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS telegram_login_attempts_recent ON telegram_login_attempts(updated_at DESC);

COMMIT;

BEGIN;

ALTER TABLE app_settings
	ADD COLUMN IF NOT EXISTS download_action_mode TEXT NOT NULL DEFAULT 'browser';

UPDATE app_settings
SET download_action_mode = 'browser'
WHERE download_action_mode IS NULL OR download_action_mode = '';

COMMIT;

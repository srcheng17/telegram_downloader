BEGIN;

CREATE TABLE IF NOT EXISTS app_settings (
	id SMALLINT PRIMARY KEY,
	timeout INTEGER NOT NULL,
	retries INTEGER NOT NULL,
	image_concurrency INTEGER NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO app_settings (
	id,
	timeout,
	retries,
	image_concurrency,
	updated_at
) VALUES (
	1,
	30,
	10,
	2,
	NOW()
)
ON CONFLICT (id) DO NOTHING;

COMMIT;

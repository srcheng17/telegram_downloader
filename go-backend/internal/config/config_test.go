package config

import "testing"

func TestLoadFromEnvIncludesRedisWorkerConfig(t *testing.T) {
	t.Setenv("PORT", "5150")
	t.Setenv(
		"DATABASE_URL",
		"postgresql://telegraph:telegraph@postgres:5432/telegraph?sslmode=disable",
	)
	t.Setenv("REDIS_URL", "redis://redis:6379/0")
	t.Setenv("STREAM_NAME", "download_tasks")
	t.Setenv("CONSUMER_GROUP", "go-workers")
	t.Setenv("CONSUMER_NAME", "")
	t.Setenv("HOSTNAME", "go-worker-1")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.RedisURL != "redis://redis:6379/0" {
		t.Fatalf("expected redis url redis://redis:6379/0, got %q", cfg.RedisURL)
	}
	if cfg.StreamName != "download_tasks" {
		t.Fatalf("expected stream name download_tasks, got %q", cfg.StreamName)
	}
	if cfg.V2StreamName != "download_tasks_v2" {
		t.Fatalf("expected default v2 stream name download_tasks_v2, got %q", cfg.V2StreamName)
	}
	if cfg.ConsumerGroup != "go-workers" {
		t.Fatalf("expected consumer group go-workers, got %q", cfg.ConsumerGroup)
	}
	if cfg.ConsumerName != "go-worker-1" {
		t.Fatalf("expected consumer name go-worker-1, got %q", cfg.ConsumerName)
	}
}

func TestLoadFromEnvReadsV2StreamOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://telegraph:telegraph@postgres:5432/telegraph?sslmode=disable")
	t.Setenv("V2_STREAM_NAME", "custom_tasks_v2")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.V2StreamName != "custom_tasks_v2" {
		t.Fatalf("expected custom v2 stream, got %q", cfg.V2StreamName)
	}
}

func TestNormalizeSettingsSnapshotClampsBounds(t *testing.T) {
	snapshot := NormalizeSettingsSnapshot(SettingsSnapshot{
		Timeout:          999,
		Retries:          -10,
		ImageConcurrency: 0,
	})

	if snapshot.Timeout != 300 {
		t.Fatalf("expected timeout clamped to 300, got %d", snapshot.Timeout)
	}
	if snapshot.Retries != 0 {
		t.Fatalf("expected retries clamped to 0, got %d", snapshot.Retries)
	}
	if snapshot.ImageConcurrency != 1 {
		t.Fatalf("expected image_concurrency clamped to 1, got %d", snapshot.ImageConcurrency)
	}
}

func TestDefaultSettingsSnapshot(t *testing.T) {
	defaults := DefaultSettingsSnapshot()
	if defaults.Timeout != 30 {
		t.Fatalf("expected default timeout 30, got %d", defaults.Timeout)
	}
	if defaults.Retries != 10 {
		t.Fatalf("expected default retries 10, got %d", defaults.Retries)
	}
	if defaults.ImageConcurrency != 2 {
		t.Fatalf("expected default image_concurrency 2, got %d", defaults.ImageConcurrency)
	}
}

func TestLoadFromEnvFailsWhenDatabaseURLMissingInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("TASKS_DB_PATH", "")
	t.Setenv("INTERNAL_ENQUEUE_TOKEN", "token-prod")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatalf("expected missing DATABASE_URL in production to fail")
	}
	if err.Error() != "DATABASE_URL is required in production" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromEnvFailsWhenInternalTokenMissingInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv(
		"DATABASE_URL",
		"postgresql://telegraph:telegraph@postgres:5432/telegraph?sslmode=disable",
	)
	t.Setenv("INTERNAL_ENQUEUE_TOKEN", "")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatalf("expected missing INTERNAL_ENQUEUE_TOKEN in production to fail")
	}
	if err.Error() != "INTERNAL_ENQUEUE_TOKEN is required in production" {
		t.Fatalf("unexpected error: %v", err)
	}
}

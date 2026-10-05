package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ryancheng/telegram-downloader/internal/config"
)

type SettingsStore struct{ pool *pgxpool.Pool }

func NewSettingsStore(pool *pgxpool.Pool) *SettingsStore { return &SettingsStore{pool: pool} }
func (s *SettingsStore) GetSettings(ctx context.Context) (config.SettingsSnapshot, error) {
	if s == nil || s.pool == nil {
		return config.SettingsSnapshot{}, errors.New("settings database is not configured")
	}
	var snapshot config.SettingsSnapshot
	err := s.pool.QueryRow(ctx, `SELECT timeout,retries,image_concurrency,download_action_mode FROM app_settings WHERE id=1`).Scan(&snapshot.Timeout, &snapshot.Retries, &snapshot.ImageConcurrency, &snapshot.DownloadActionMode)
	if errors.Is(err, pgx.ErrNoRows) {
		return config.DefaultSettingsSnapshot(), nil
	}
	if err != nil {
		return config.SettingsSnapshot{}, err
	}
	return config.NormalizeSettingsSnapshot(snapshot), nil
}
func (s *SettingsStore) UpdateSettings(ctx context.Context, snapshot config.SettingsSnapshot) (config.SettingsSnapshot, error) {
	if s == nil || s.pool == nil {
		return config.SettingsSnapshot{}, errors.New("settings database is not configured")
	}
	snapshot = config.NormalizeSettingsSnapshot(snapshot)
	_, err := s.pool.Exec(ctx, `INSERT INTO app_settings(id,timeout,retries,image_concurrency,download_action_mode,config_version,updated_at) VALUES(1,$1,$2,$3,$4,1,NOW()) ON CONFLICT(id) DO UPDATE SET timeout=EXCLUDED.timeout,retries=EXCLUDED.retries,image_concurrency=EXCLUDED.image_concurrency,download_action_mode=EXCLUDED.download_action_mode,config_version=app_settings.config_version+1,updated_at=NOW()`, snapshot.Timeout, snapshot.Retries, snapshot.ImageConcurrency, snapshot.DownloadActionMode)
	return snapshot, err
}

func (s *SettingsStore) GetDownloadSettings(ctx context.Context) (config.VersionedSettingsSnapshot, error) {
	if s == nil || s.pool == nil {
		return config.VersionedSettingsSnapshot{}, errors.New("settings database is not configured")
	}
	var result config.VersionedSettingsSnapshot
	err := s.pool.QueryRow(ctx, `SELECT timeout,retries,image_concurrency,download_action_mode,config_version FROM app_settings WHERE id=1`).Scan(
		&result.Timeout, &result.Retries, &result.ImageConcurrency, &result.DownloadActionMode, &result.ConfigVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return config.VersionedSettingsSnapshot{SettingsSnapshot: config.DefaultSettingsSnapshot()}, nil
	}
	if err != nil {
		return config.VersionedSettingsSnapshot{}, err
	}
	result.SettingsSnapshot = config.NormalizeSettingsSnapshot(result.SettingsSnapshot)
	return result, nil
}

func (s *SettingsStore) UpdateDownloadSettings(ctx context.Context, snapshot config.SettingsSnapshot, expectedVersion int64) (config.VersionedSettingsSnapshot, error) {
	if s == nil || s.pool == nil {
		return config.VersionedSettingsSnapshot{}, errors.New("settings database is not configured")
	}
	if expectedVersion < 0 || config.NormalizeSettingsSnapshot(snapshot) != snapshot {
		return config.VersionedSettingsSnapshot{}, config.ErrInvalidSettings
	}
	var version int64
	err := s.pool.QueryRow(ctx, `UPDATE app_settings
		SET timeout=$1,retries=$2,image_concurrency=$3,download_action_mode=$4,config_version=config_version+1,updated_at=NOW()
		WHERE id=1 AND config_version=$5 RETURNING config_version`, snapshot.Timeout, snapshot.Retries, snapshot.ImageConcurrency, snapshot.DownloadActionMode, expectedVersion).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return config.VersionedSettingsSnapshot{}, config.ErrSettingsConflict
	}
	if err != nil {
		return config.VersionedSettingsSnapshot{}, err
	}
	return config.VersionedSettingsSnapshot{SettingsSnapshot: snapshot, ConfigVersion: version}, nil
}

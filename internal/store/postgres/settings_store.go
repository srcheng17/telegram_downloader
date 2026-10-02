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
	_, err := s.pool.Exec(ctx, `INSERT INTO app_settings(id,timeout,retries,image_concurrency,download_action_mode,updated_at) VALUES(1,$1,$2,$3,$4,NOW()) ON CONFLICT(id) DO UPDATE SET timeout=EXCLUDED.timeout,retries=EXCLUDED.retries,image_concurrency=EXCLUDED.image_concurrency,download_action_mode=EXCLUDED.download_action_mode,updated_at=NOW()`, snapshot.Timeout, snapshot.Retries, snapshot.ImageConcurrency, snapshot.DownloadActionMode)
	return snapshot, err
}

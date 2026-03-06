package httpv2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/config"
)

type SettingsStore interface {
	GetSettings(ctx context.Context) (config.SettingsSnapshot, error)
	UpdateSettings(ctx context.Context, snapshot config.SettingsSnapshot) (config.SettingsSnapshot, error)
}

type SettingsHandler struct {
	store SettingsStore
}

func NewSettingsHandler(store SettingsStore) *SettingsHandler {
	return &SettingsHandler{store: store}
}

func RegisterSettingsRoutes(r chi.Router, h *SettingsHandler) {
	if r == nil {
		return
	}
	if h == nil {
		h = &SettingsHandler{}
	}
	r.Get("/v2/settings", h.GetSettings)
	r.Put("/v2/settings", h.UpdateSettings)
}

func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusInternalServerError, "settings store is not configured")
		return
	}

	snapshot, err := h.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get settings")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusInternalServerError, "settings store is not configured")
		return
	}

	var payload config.SettingsSnapshot
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	normalized := config.NormalizeSettingsSnapshot(payload)
	snapshot, err := h.store.UpdateSettings(r.Context(), normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update settings")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

type appSettingsExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func (s *PostgresTaskStore) GetSettings(ctx context.Context) (config.SettingsSnapshot, error) {
	if err := s.ensureAppSettingsTable(ctx); err != nil {
		return config.SettingsSnapshot{}, err
	}

	var snapshot config.SettingsSnapshot
	err := s.db.QueryRow(
		ctx,
		`
		SELECT timeout, retries, image_concurrency
		FROM app_settings
		WHERE id = 1
		`,
	).Scan(&snapshot.Timeout, &snapshot.Retries, &snapshot.ImageConcurrency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return config.DefaultSettingsSnapshot(), nil
		}
		return config.SettingsSnapshot{}, err
	}
	return config.NormalizeSettingsSnapshot(snapshot), nil
}

func (s *PostgresTaskStore) UpdateSettings(ctx context.Context, snapshot config.SettingsSnapshot) (config.SettingsSnapshot, error) {
	if err := s.ensureAppSettingsTable(ctx); err != nil {
		return config.SettingsSnapshot{}, err
	}

	normalized := config.NormalizeSettingsSnapshot(snapshot)
	execer, err := s.settingsExecer()
	if err != nil {
		return config.SettingsSnapshot{}, err
	}

	_, err = execer.Exec(
		ctx,
		`
		INSERT INTO app_settings (
			id, timeout, retries, image_concurrency, updated_at
		) VALUES (
			1, $1, $2, $3, NOW()
		)
		ON CONFLICT (id) DO UPDATE
		SET
			timeout = EXCLUDED.timeout,
			retries = EXCLUDED.retries,
			image_concurrency = EXCLUDED.image_concurrency,
			updated_at = NOW()
		`,
		normalized.Timeout,
		normalized.Retries,
		normalized.ImageConcurrency,
	)
	if err != nil {
		return config.SettingsSnapshot{}, err
	}

	return normalized, nil
}

func (s *PostgresTaskStore) ensureAppSettingsTable(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("v2 task database is not configured")
	}
	execer, err := s.settingsExecer()
	if err != nil {
		return err
	}

	_, err = execer.Exec(
		ctx,
		`
		CREATE TABLE IF NOT EXISTS app_settings (
			id SMALLINT PRIMARY KEY,
			timeout INTEGER NOT NULL,
			retries INTEGER NOT NULL,
			image_concurrency INTEGER NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
		`,
	)
	if err != nil {
		return err
	}

	defaults := config.DefaultSettingsSnapshot()
	_, err = execer.Exec(
		ctx,
		`
		INSERT INTO app_settings (
			id, timeout, retries, image_concurrency, updated_at
		) VALUES (
			1, $1, $2, $3, NOW()
		)
		ON CONFLICT (id) DO NOTHING
		`,
		defaults.Timeout,
		defaults.Retries,
		defaults.ImageConcurrency,
	)
	return err
}

func (s *PostgresTaskStore) settingsExecer() (appSettingsExecer, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("v2 task database is not configured")
	}

	execer, ok := s.db.(appSettingsExecer)
	if !ok {
		return nil, errors.New("v2 task database does not support settings writes")
	}
	return execer, nil
}

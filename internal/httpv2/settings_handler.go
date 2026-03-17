package httpv2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ryancheng/telegram-downloader/internal/config"
)

type SettingsStore interface {
	GetSettings(ctx context.Context) (config.SettingsSnapshot, error)
	UpdateSettings(ctx context.Context, snapshot config.SettingsSnapshot) (config.SettingsSnapshot, error)
}

type SettingsHandler struct {
	store SettingsStore
}

type settingsUpdatePayload struct {
	Timeout          *int `json:"timeout"`
	Retries          *int `json:"retries"`
	ImageConcurrency *int `json:"image_concurrency"`
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

	var payload settingsUpdatePayload
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if payload.Timeout == nil || payload.Retries == nil || payload.ImageConcurrency == nil {
		writeError(w, http.StatusBadRequest, "timeout, retries, image_concurrency are required")
		return
	}

	normalized := config.NormalizeSettingsSnapshot(config.SettingsSnapshot{
		Timeout:          *payload.Timeout,
		Retries:          *payload.Retries,
		ImageConcurrency: *payload.ImageConcurrency,
	})
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

var appSettingsInit sync.Map

func (s *PostgresTaskStore) GetSettings(ctx context.Context) (config.SettingsSnapshot, error) {
	if err := s.ensureAppSettingsDefaultRow(ctx); err != nil {
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
	if err := s.ensureAppSettingsDefaultRow(ctx); err != nil {
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

func (s *PostgresTaskStore) ensureAppSettingsDefaultRow(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("v2 task database is not configured")
	}
	execer, err := s.settingsExecer()
	if err != nil {
		return err
	}

	key := execer
	if _, ok := appSettingsInit.Load(key); ok {
		return nil
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
	if err != nil {
		return err
	}
	appSettingsInit.Store(key, struct{}{})
	return nil
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

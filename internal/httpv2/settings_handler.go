package httpv2

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

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
	Timeout            *int    `json:"timeout"`
	Retries            *int    `json:"retries"`
	ImageConcurrency   *int    `json:"image_concurrency"`
	DownloadActionMode *string `json:"download_action_mode"`
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
	if payload.Timeout == nil || payload.Retries == nil || payload.ImageConcurrency == nil || payload.DownloadActionMode == nil {
		writeError(w, http.StatusBadRequest, "timeout, retries, image_concurrency, download_action_mode are required")
		return
	}

	normalized := config.NormalizeSettingsSnapshot(config.SettingsSnapshot{
		Timeout:            *payload.Timeout,
		Retries:            *payload.Retries,
		ImageConcurrency:   *payload.ImageConcurrency,
		DownloadActionMode: *payload.DownloadActionMode,
	})
	snapshot, err := h.store.UpdateSettings(r.Context(), normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update settings")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

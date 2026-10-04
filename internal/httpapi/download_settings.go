package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/config"
)

type DownloadSettingsStore interface {
	GetDownloadSettings(context.Context) (config.VersionedSettingsSnapshot, error)
	UpdateDownloadSettings(context.Context, config.SettingsSnapshot, int64) (config.VersionedSettingsSnapshot, error)
}

type DownloadSettingsHandler struct{ store DownloadSettingsStore }

func NewDownloadSettingsHandler(store DownloadSettingsStore) *DownloadSettingsHandler {
	return &DownloadSettingsHandler{store: store}
}

func (h *DownloadSettingsHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/settings/download", h.get)
	r.Put("/api/settings/download", h.put)
}

func (h *DownloadSettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.store == nil {
		writeAPIErrorResponse(w, http.StatusServiceUnavailable, "unavailable", "下载设置暂不可用。", nil)
		return
	}
	view, err := h.store.GetDownloadSettings(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *DownloadSettingsHandler) put(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.store == nil {
		writeAPIErrorResponse(w, http.StatusServiceUnavailable, "unavailable", "下载设置暂不可用。", nil)
		return
	}
	var input struct {
		Timeout            *int    `json:"timeout"`
		Retries            *int    `json:"retries"`
		ImageConcurrency   *int    `json:"image_concurrency"`
		DownloadActionMode *string `json:"download_action_mode"`
		ExpectedVersion    *int64  `json:"expected_version"`
	}
	if strictJSON(w, r, &input) != nil || input.Timeout == nil || input.Retries == nil || input.ImageConcurrency == nil || input.DownloadActionMode == nil || input.ExpectedVersion == nil {
		writeAPIErrorResponse(w, http.StatusUnprocessableEntity, "invalid_input", "下载设置不完整。", nil)
		return
	}
	snapshot := config.SettingsSnapshot{
		Timeout: *input.Timeout, Retries: *input.Retries,
		ImageConcurrency: *input.ImageConcurrency, DownloadActionMode: *input.DownloadActionMode,
	}
	view, err := h.store.UpdateDownloadSettings(r.Context(), snapshot, *input.ExpectedVersion)
	switch {
	case errors.Is(err, config.ErrInvalidSettings):
		writeAPIErrorResponse(w, http.StatusUnprocessableEntity, "invalid_input", "下载设置超出允许范围。", nil)
	case errors.Is(err, config.ErrSettingsConflict):
		writeAPIErrorResponse(w, http.StatusConflict, "config_conflict", "下载设置已变化，请重新读取后保存。", nil)
	case err != nil:
		writeInternalError(w, err)
	default:
		writeJSON(w, http.StatusOK, view)
	}
}

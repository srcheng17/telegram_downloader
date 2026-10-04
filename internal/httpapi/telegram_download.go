package httpapi

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/app/telegram"
	"github.com/ryancheng/telegram-downloader/internal/config"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"net/http"
)

func RegisterTelegramDownloads(r chi.Router, account *telegram.Service, tasks *app.Service, settings TaskSettingsProvider) {
	r.Post("/api/telegram/downloads", func(w http.ResponseWriter, r *http.Request) {
		if account == nil {
			telegramError(w, telegram.Failure("telegram_unavailable"))
			return
		}
		var input struct {
			MessageURL string             `json:"message_url"`
			Force      bool               `json:"force"`
			Document   *metadata.Document `json:"metadata_document"`
		}
		if !readMetadataJSON(w, r, &input) {
			return
		}
		source, err := account.SnapshotForTask(r.Context(), input.MessageURL)
		if err != nil {
			telegramError(w, err)
			return
		}
		snapshot := config.DefaultSettingsSnapshot()
		if settings != nil {
			snapshot, err = settings.GetSettings(r.Context())
			if err != nil {
				writeInternalError(w, err)
				return
			}
		}
		created, err := tasks.CreateTelegramTask(r.Context(), app.CreateTelegramInput{ID: uuid.NewString(), Source: source, MetadataDocument: input.Document, Force: input.Force, RuntimeSettings: &snapshot})
		if err != nil {
			writeMetadataError(w, err)
			return
		}
		payload := map[string]any{"ok": true, "task_id": created.ID, "id": created.ID, "status": created.Status, "logs_url": "/logs"}
		status := http.StatusAccepted
		if created.Reused {
			payload["duplicate"] = true
			payload["active"] = !created.NeedsConfirmation
		}
		if created.NeedsConfirmation {
			status = http.StatusOK
			payload["needs_confirmation"] = true
			payload["download_url"] = "/api/tasks/" + created.ID + "/download"
		}
		writeJSON(w, status, payload)
	})
}

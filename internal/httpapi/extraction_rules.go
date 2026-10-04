package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	app "github.com/ryancheng/telegram-downloader/internal/app/extractionrules"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

func RegisterExtractionRules(r chi.Router, service *app.Service) {
	r.Get("/api/settings/extraction-rules", func(w http.ResponseWriter, r *http.Request) {
		set, err := service.Get(r.Context())
		if err != nil {
			writeRulesError(w, err)
			return
		}
		writeJSON(w, 200, set)
	})
	r.Put("/api/settings/extraction-rules", func(w http.ResponseWriter, r *http.Request) {
		var input app.Update
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, app.MaxBytes))
		if err != nil || domain.DecodeJSON(data, &input) != nil {
			writeRulesError(w, app.ErrInvalid)
			return
		}
		set, err := service.Save(r.Context(), input)
		if err != nil {
			writeRulesError(w, err)
			return
		}
		writeJSON(w, 200, set)
	})
}
func writeRulesError(w http.ResponseWriter, err error) {
	if errors.Is(err, app.ErrConflict) {
		writeAPIErrorResponse(w, 409, "rules_conflict", "规则或字段定义已更新，请重新读取后保存。", nil)
		return
	}
	if errors.Is(err, app.ErrInvalid) {
		writeAPIErrorResponse(w, 400, "invalid_rules", "请核对规则字段、标签、类型及长度。", nil)
		return
	}
	writeInternalError(w, err)
}

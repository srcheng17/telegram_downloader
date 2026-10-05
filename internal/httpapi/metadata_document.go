package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	app "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

// RegisterMetadata exposes only document operations; the root administrator
// middleware protects these routes together with all other business routes.
func RegisterMetadata(r chi.Router, service *app.Service) {
	r.Get("/api/metadata/schema", func(w http.ResponseWriter, req *http.Request) {
		result, err := service.Schema(req.Context())
		if err != nil {
			writeMetadataError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	r.Get("/api/settings/metadata-fields", func(w http.ResponseWriter, req *http.Request) {
		result, err := service.Fields(req.Context())
		if err != nil {
			writeMetadataError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	r.Put("/api/settings/metadata-fields", func(w http.ResponseWriter, req *http.Request) {
		var input app.UpdateFieldsInput
		if !readMetadataJSON(w, req, &input) {
			return
		}
		result, err := service.UpdateFields(req.Context(), input)
		if err != nil {
			writeMetadataError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	r.Post("/api/metadata/validate", func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			Document domain.Document `json:"document"`
		}
		if !readMetadataJSON(w, req, &input) {
			return
		}
		result, err := service.Validate(req.Context(), input.Document)
		if err != nil {
			writeMetadataError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	r.Post("/api/metadata/patch", func(w http.ResponseWriter, req *http.Request) {
		var input app.PatchInput
		if !readMetadataJSON(w, req, &input) {
			return
		}
		result, err := service.Patch(req.Context(), input)
		if err != nil {
			writeMetadataError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func readMetadataJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err == nil {
		err = domain.DecodeJSON(data, dst)
	}
	if err != nil {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "元数据请求格式无效。", nil)
		return false
	}
	return true
}

func writeMetadataError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrConflict) {
		writeAPIErrorResponse(w, http.StatusConflict, "metadata_conflict", "内容已更新，请核对后重试。", nil)
		return
	}
	if errors.Is(err, domain.ErrInvalidInput) {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "元数据不符合字段定义，请核对版本、类型及长度。", nil)
		return
	}
	writeInternalError(w, err)
}

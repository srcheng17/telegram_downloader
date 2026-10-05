package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/komgaedit"
)

type KomgaEditService interface {
	Detail(context.Context, string) (komgaedit.EditDetail, error)
	Preview(context.Context, string, komgaedit.PreviewRequest) (komgaedit.PreviewResult, error)
	Save(context.Context, string, komgaedit.SaveRequest) (komgaedit.OperationView, error)
	Operation(context.Context, string) (komgaedit.OperationView, error)
	RetrySync(context.Context, string) (komgaedit.OperationView, error)
	Restore(context.Context, string) (komgaedit.OperationView, error)
}

type KomgaEditHandler struct{ service KomgaEditService }

func NewKomgaEditHandler(service KomgaEditService) *KomgaEditHandler {
	return &KomgaEditHandler{service: service}
}

func (h *KomgaEditHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/komga/books/{id}/edit", h.detail)
	r.Post("/api/komga/books/{id}/preview", h.preview)
	r.Post("/api/komga/books/{id}/save", h.save)
	r.Get("/api/komga/edits/{operationId}", h.operation)
	r.Post("/api/komga/edits/{operationId}/sync", h.sync)
	r.Post("/api/komga/edits/{operationId}/restore", h.restore)
}

func (h *KomgaEditHandler) detail(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeKomgaError(w, komgaedit.ErrDisabled)
		return
	}
	view, err := h.service.Detail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeKomgaEditError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *KomgaEditHandler) preview(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeKomgaError(w, komgaedit.ErrDisabled)
		return
	}
	var input komgaedit.PreviewRequest
	if strictJSON(w, r, &input) != nil {
		writeKomgaEditError(w, komgaedit.ErrInvalid)
		return
	}
	view, err := h.service.Preview(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		writeKomgaEditError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *KomgaEditHandler) save(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeKomgaError(w, komgaedit.ErrDisabled)
		return
	}
	var input komgaedit.SaveRequest
	if strictJSON(w, r, &input) != nil {
		writeKomgaEditError(w, komgaedit.ErrInvalid)
		return
	}
	view, err := h.service.Save(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		writeKomgaEditError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operation": view})
}

func (h *KomgaEditHandler) operation(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeKomgaError(w, komgaedit.ErrDisabled)
		return
	}
	view, err := h.service.Operation(r.Context(), chi.URLParam(r, "operationId"))
	if err != nil {
		writeKomgaEditError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operation": view})
}

func (h *KomgaEditHandler) sync(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeKomgaError(w, komgaedit.ErrDisabled)
		return
	}
	var input struct{}
	if strictJSON(w, r, &input) != nil {
		writeKomgaEditError(w, komgaedit.ErrInvalid)
		return
	}
	view, err := h.service.RetrySync(r.Context(), chi.URLParam(r, "operationId"))
	if err != nil {
		writeKomgaEditError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operation": view})
}

func (h *KomgaEditHandler) restore(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeKomgaError(w, komgaedit.ErrDisabled)
		return
	}
	var input struct{}
	if strictJSON(w, r, &input) != nil {
		writeKomgaEditError(w, komgaedit.ErrInvalid)
		return
	}
	view, err := h.service.Restore(r.Context(), chi.URLParam(r, "operationId"))
	if err != nil {
		writeKomgaEditError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operation": view})
}

func writeKomgaEditError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, komgaedit.ErrConflict):
		writeAPIErrorResponse(w, http.StatusConflict, "edit_conflict", "文件或字段定义已变化，请重新读取后预览。", nil)
	case errors.Is(err, komgaedit.ErrUnsafe):
		writeAPIErrorResponse(w, http.StatusConflict, "book_unavailable", "该作品无法安全写回，请核对文件和书库配置。", nil)
	default:
		writeKomgaError(w, err)
	}
}

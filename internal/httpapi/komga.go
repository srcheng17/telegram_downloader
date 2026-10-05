package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/komgaedit"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

type KomgaHandler struct {
	connection *komgaedit.ConnectionService
	catalog    *komgaedit.CatalogService
}

func NewKomgaHandler(connection *komgaedit.ConnectionService, catalog *komgaedit.CatalogService) *KomgaHandler {
	return &KomgaHandler{connection: connection, catalog: catalog}
}

func (h *KomgaHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/settings/komga", h.settings)
	r.Put("/api/settings/komga", h.saveSettings)
	r.Post("/api/settings/komga/test", h.testSettings)
	r.Get("/api/komga/libraries", h.libraries)
	r.Get("/api/komga/books", h.books)
	r.Get("/api/komga/books/{id}", h.book)
}

func (h *KomgaHandler) settings(w http.ResponseWriter, r *http.Request) {
	view, err := h.connection.Get(r.Context())
	if err != nil {
		writeKomgaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *KomgaHandler) saveSettings(w http.ResponseWriter, r *http.Request) {
	var input komgaedit.ConnectionUpdate
	if strictJSON(w, r, &input) != nil {
		writeKomgaError(w, komgaedit.ErrInvalid)
		return
	}
	view, err := h.connection.Save(r.Context(), input)
	if err != nil {
		writeKomgaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *KomgaHandler) testSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ConfigVersion *int64 `json:"config_version"`
	}
	if strictJSON(w, r, &input) != nil || input.ConfigVersion == nil {
		writeKomgaError(w, komgaedit.ErrInvalid)
		return
	}
	config, err := h.connection.Get(r.Context())
	if err != nil {
		writeKomgaError(w, err)
		return
	}
	if config.ConfigVersion != *input.ConfigVersion {
		writeKomgaError(w, komgaedit.ErrConflict)
		return
	}
	libraries, err := h.catalog.Libraries(r.Context())
	if err != nil {
		writeKomgaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "connected", "allowed_library_count": len(libraries), "checked_at": time.Now().UTC()})
}

func (h *KomgaHandler) libraries(w http.ResponseWriter, r *http.Request) {
	items, err := h.catalog.Libraries(r.Context())
	if err != nil {
		writeKomgaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": items})
}

func (h *KomgaHandler) books(w http.ResponseWriter, r *http.Request) {
	page := 0
	var pageErr error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, pageErr = strconv.Atoi(raw)
	}
	size := 25
	var sizeErr error
	if raw := r.URL.Query().Get("size"); raw != "" {
		size, sizeErr = strconv.Atoi(raw)
	}
	if pageErr != nil || sizeErr != nil {
		writeKomgaError(w, komgaedit.ErrInvalid)
		return
	}
	result, err := h.catalog.Books(r.Context(), r.URL.Query().Get("library_id"), r.URL.Query().Get("query"), page, size)
	if err != nil {
		writeKomgaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *KomgaHandler) book(w http.ResponseWriter, r *http.Request) {
	view, err := h.catalog.Book(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeKomgaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func writeKomgaError(w http.ResponseWriter, err error) {
	var upstream *komga.Error
	switch {
	case errors.Is(err, komgaedit.ErrInvalid):
		writeAPIErrorResponse(w, http.StatusUnprocessableEntity, "invalid_input", "Komga 请求无效，请检查书库或设置。", nil)
	case errors.Is(err, komgaedit.ErrConflict):
		writeAPIErrorResponse(w, http.StatusConflict, "config_conflict", "Komga 设置已变化，请重新加载。", nil)
	case errors.Is(err, komgaedit.ErrNotFound):
		writeAPIErrorResponse(w, http.StatusNotFound, "not_found", "未找到该作品。", nil)
	case errors.Is(err, komgaedit.ErrDisabled):
		writeAPIErrorResponse(w, http.StatusServiceUnavailable, "komga_not_configured", "请先配置 Komga 连接。", nil)
	case errors.Is(err, komgaedit.ErrUnsafe):
		writeAPIErrorResponse(w, http.StatusConflict, "book_unavailable", "该作品文件不可安全编辑。", nil)
	case errors.As(err, &upstream) && upstream.Code == "not_found":
		writeAPIErrorResponse(w, http.StatusNotFound, "not_found", "未找到该作品。", nil)
	case errors.As(err, &upstream):
		writeAPIErrorResponse(w, http.StatusBadGateway, upstream.Code, "Komga 请求未完成，请检查连接或稍后重试。", nil)
	default:
		writeInternalError(w, err)
	}
}

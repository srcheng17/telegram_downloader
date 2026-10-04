package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/metadatasearch"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type MetadataSearchHandler struct{ service *metadatasearch.Service }

func NewMetadataSearchHandler(service *metadatasearch.Service) *MetadataSearchHandler {
	return &MetadataSearchHandler{service}
}

// RegisterRoutes must be called inside the shared administrator/CSRF boundary.
func (h *MetadataSearchHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/metadata/providers", h.Providers)
	r.Post("/api/metadata/search", h.Search)
	r.Post("/api/metadata/candidates/resolve", h.Resolve)
}
func metadataSearchError(w http.ResponseWriter, err error) {
	var source *metadatasearch.Error
	switch {
	case errors.Is(err, metadatasearch.ErrInvalid):
		writeAPIErrorResponse(w, 422, "invalid_search", "请检查关键词、来源选择、字段映射和版本", nil)
	case errors.Is(err, metadatasearch.ErrConflict):
		writeAPIErrorResponse(w, 409, "search_conflict", "来源配置或字段定义已变化，请重新搜索", nil)
	case errors.As(err, &source):
		writeAPIErrorResponse(w, 502, source.Code, "来源请求未完成，请检查该来源的状态后重试", map[string]any{"retry_after": source.RetryAfter})
	default:
		writeAPIErrorResponse(w, 503, "unavailable", "书目来源服务暂不可用", nil)
	}
}
func (h *MetadataSearchHandler) Providers(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Providers(r.Context())
	if err != nil {
		metadataSearchError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"sources": result})
}
func (h *MetadataSearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	var input metadatasearch.SearchInput
	if !decodeSearchJSON(w, r, &input, "keyword", "provider_ids", "query_revision") {
		metadataSearchError(w, metadatasearch.ErrInvalid)
		return
	}
	result, err := h.service.Search(r.Context(), input)
	if err != nil {
		metadataSearchError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *MetadataSearchHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	var input metadatasearch.ResolveInput
	if !decodeSearchJSON(w, r, &input, "provider_id", "record_id", "query_revision", "base_document_revision", "field_revisions", "schema_version", "definitions_version", "config_version") {
		metadataSearchError(w, metadatasearch.ErrInvalid)
		return
	}
	result, err := h.service.Resolve(r.Context(), input)
	if err != nil {
		metadataSearchError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func decodeSearchJSON(w http.ResponseWriter, r *http.Request, dest any, required ...string) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || metadata.DecodeJSON(data, dest) != nil {
		return false
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(data, &members) != nil {
		return false
	}
	for _, key := range required {
		raw, ok := members[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false
		}
	}
	if raw, ok := members["field_revisions"]; ok {
		var revisions map[string]json.RawMessage
		if json.Unmarshal(raw, &revisions) != nil {
			return false
		}
		for _, value := range revisions {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return false
			}
		}
	}
	return true
}

package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/metadatasearch"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/modelapi"
	"net/http"
)

type SourceSettings struct{ service *sourcesettings.Service }

func NewSourceSettings(service *sourcesettings.Service) *SourceSettings {
	return &SourceSettings{service: service}
}
func (s *SourceSettings) RegisterRoutes(r chi.Router) {
	r.Get("/api/settings/sources", s.sources)
	r.Put("/api/settings/sources/{provider}", s.saveSource)
	r.Post("/api/settings/sources/{provider}/test", s.testSource)
	r.Get("/api/settings/ai", s.ai)
	r.Put("/api/settings/ai", s.saveAI)
	r.Post("/api/settings/ai/models", s.models)
	r.Post("/api/settings/ai/test", s.testAI)
}
func settingsError(w http.ResponseWriter, err error) {
	var upstream *modelapi.Error
	var provider *metadatasearch.Error
	switch {
	case errors.Is(err, sourcesettings.ErrInvalid):
		writeAPIErrorResponse(w, 422, "invalid_configuration", "设置无效，请检查必填项、凭据操作及支持的选项", nil)
	case errors.Is(err, sourcesettings.ErrConflict):
		writeAPIErrorResponse(w, 409, "config_conflict", "设置已发生变化，请重新加载后重试", nil)
	case errors.Is(err, sourcesettings.ErrNotFound):
		writeAPIErrorResponse(w, 404, "not_found", "来源不存在", nil)
	case errors.Is(err, credentials.ErrSecret):
		writeAPIErrorResponse(w, 503, "credential_unavailable", "凭据暂不可用，请检查服务器密钥配置", nil)
	case errors.As(err, &upstream):
		writeAPIErrorResponse(w, 502, upstream.Code, "外部服务验证未通过", nil)
	case errors.As(err, &provider):
		status := http.StatusBadGateway
		if provider.Code == "rate_limited" {
			status = http.StatusTooManyRequests
		}
		writeAPIErrorResponse(w, status, provider.Code, "采集源验证未通过，请检查配置或稍后重试。", nil)
	default:
		writeAPIErrorResponse(w, 503, "unavailable", "设置服务暂不可用", nil)
	}
}
func (s *SourceSettings) sources(w http.ResponseWriter, r *http.Request) {
	v, err := s.service.Sources(r.Context())
	if err != nil {
		settingsError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"sources": v})
}
func (s *SourceSettings) saveSource(w http.ResponseWriter, r *http.Request) {
	var input sourcesettings.SourceUpdate
	if strictJSON(w, r, &input) != nil {
		settingsError(w, sourcesettings.ErrInvalid)
		return
	}
	v, err := s.service.SaveSource(r.Context(), chi.URLParam(r, "provider"), input)
	if err != nil {
		settingsError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *SourceSettings) testSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version *int64 `json:"config_version"`
	}
	if strictJSON(w, r, &input) != nil || input.Version == nil {
		settingsError(w, sourcesettings.ErrInvalid)
		return
	}
	v, err := s.service.TestSource(r.Context(), chi.URLParam(r, "provider"), *input.Version)
	if err != nil {
		settingsError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *SourceSettings) ai(w http.ResponseWriter, r *http.Request) {
	v, err := s.service.AI(r.Context())
	if err != nil {
		settingsError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *SourceSettings) saveAI(w http.ResponseWriter, r *http.Request) {
	var input sourcesettings.AIUpdate
	if strictJSON(w, r, &input) != nil {
		settingsError(w, sourcesettings.ErrInvalid)
		return
	}
	v, err := s.service.SaveAI(r.Context(), input)
	if err != nil {
		settingsError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *SourceSettings) models(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version *int64 `json:"config_version"`
	}
	if strictJSON(w, r, &input) != nil || input.Version == nil {
		settingsError(w, sourcesettings.ErrInvalid)
		return
	}
	v, err := s.service.Models(r.Context(), *input.Version)
	if err != nil {
		settingsError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *SourceSettings) testAI(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version *int64 `json:"config_version"`
		Model   string `json:"model_id"`
	}
	if strictJSON(w, r, &input) != nil || input.Version == nil {
		settingsError(w, sourcesettings.ErrInvalid)
		return
	}
	v, err := s.service.TestAI(r.Context(), *input.Version, input.Model)
	if err != nil {
		settingsError(w, err)
		return
	}
	writeJSON(w, 200, v)
}

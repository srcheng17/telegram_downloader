package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	app "github.com/ryancheng/telegram-downloader/internal/app/metadataextract"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

// RegisterMetadataExtract is mounted under the shared admin and CSRF middleware.
func RegisterMetadataExtract(r chi.Router, service *app.Service) {
	r.Post("/api/metadata/extract", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		data, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 128*1024))
		var input app.Input
		if err != nil || domain.DecodeJSON(data, &input) != nil {
			writeExtractionError(w, app.Failure("invalid_request"))
			return
		}
		result, err := service.Extract(req.Context(), input)
		if err != nil {
			writeExtractionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
func writeExtractionError(w http.ResponseWriter, err error) {
	code := "unreachable"
	var typed *app.Error
	if errors.As(err, &typed) {
		code = typed.Code
	}
	status := http.StatusBadGateway
	messages := map[string]string{
		"disabled": "AI 提取已停用，校对文字仍然保留。", "not_configured": "请先保存 AI 服务和模型设置。", "config_changed": "AI 设置已变化，请重新核对发送目标。",
		"schema_unsupported": "此模型尚不支持所选字段或已验证的完整上下文预算。", "input_too_large": "文字超过允许长度，请手工选择本次发送内容。", "context_exceeded": "完整请求超出模型上下文，请减少发送文字或字段。",
		"refused": "模型拒绝了本次提取，已有文字与草稿未改变。", "unauthorized": "AI 服务凭据无效，请检查设置。", "forbidden": "AI 服务拒绝访问，请检查权限。", "rate_limited": "AI 服务请求过于频繁，请稍后手动重试。",
		"timeout": "AI 提取超时，已有文字仍然保留。", "unreachable": "暂时无法连接 AI 服务。", "invalid_response": "AI 结果缺少有效证据或不符合字段格式，未生成可采用建议。", "cancelled": "本次 AI 提取已取消。", "invalid_request": "提取请求无效，请检查文字、字段和版本。",
	}
	switch code {
	case "invalid_request", "input_too_large", "context_exceeded", "schema_unsupported":
		status = http.StatusBadRequest
	case "config_changed":
		status = http.StatusConflict
	case "disabled", "not_configured":
		status = http.StatusServiceUnavailable
	case "rate_limited":
		status = http.StatusTooManyRequests
	case "timeout":
		status = http.StatusGatewayTimeout
	case "cancelled":
		status = 499
	}
	message, ok := messages[code]
	if !ok {
		code = "unreachable"
		message = messages[code]
	}
	// Never pass upstream errors to the generic logger: they may contain model text.
	writeAPIErrorResponse(w, status, code, message, nil)
}

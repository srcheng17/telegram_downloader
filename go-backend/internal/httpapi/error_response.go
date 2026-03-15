package httpapi

import (
	"net/http"
	"strings"
)

const (
	apiErrorCodeValidation            = "validation_error"
	apiErrorCodeRequest               = "request_error"
	apiErrorCodeInternal              = "internal_error"
	apiErrorCodeEnqueueFailed         = "enqueue_failed"
	apiErrorCodeServiceUnavailable    = "service_unavailable"
	apiErrorCodeTaskNotFound          = "task_not_found"
	apiErrorCodeTaskNotReady          = "task_not_ready"
	apiErrorCodeTaskAlreadyDone       = "task_already_finished"
	apiErrorCodeArtifactNotFound      = "artifact_not_found"
	apiErrorCodeArtifactUnavailable   = "artifact_unavailable"
	apiErrorCodeUpstreamUnavailable   = "upstream_unavailable"
	apiErrorCodeUpstreamBuildFailed   = "upstream_request_build_failed"
	apiErrorCodeUpstreamRequestFailed = "upstream_request_failed"
)

type apiErrorPayload struct {
	Error   string         `json:"error"`
	Code    string         `json:"code"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

func writeAPIErrorResponse(w http.ResponseWriter, statusCode int, code string, message string, details map[string]any) {
	normalizedCode := normalizeAPIErrorCode(statusCode, code)
	normalizedMessage := normalizeAPIErrorMessage(statusCode, normalizedCode, message)

	payload := apiErrorPayload{
		Error: normalizedMessage,
		Code:  normalizedCode,
	}
	if normalizedMessage != "" {
		payload.Message = normalizedMessage
	}
	if len(details) > 0 {
		payload.Details = details
	}

	writeJSON(w, statusCode, payload)
}

func normalizeAPIErrorCode(statusCode int, code string) string {
	trimmed := strings.TrimSpace(code)
	if trimmed != "" {
		return trimmed
	}
	if statusCode >= http.StatusInternalServerError {
		return apiErrorCodeInternal
	}
	return apiErrorCodeRequest
}

func normalizeAPIErrorMessage(statusCode int, code string, message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed != "" {
		return trimmed
	}
	if code == apiErrorCodeInternal || statusCode >= http.StatusInternalServerError {
		return "internal server error"
	}
	return "request failed"
}

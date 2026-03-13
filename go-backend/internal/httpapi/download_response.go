package httpapi

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
)

func buildClaimDownloadDecisionResponse(taskID string, decision domain.ClaimDecision, forceApplied bool) (int, map[string]any, error) {
	return buildDownloadDecisionResponse(taskID, string(decision), forceApplied)
}

func buildLegacyDownloadDecisionResponse(taskID string, decision LegacyDownloadDecision, forceApplied bool) (int, map[string]any, error) {
	return buildDownloadDecisionResponse(taskID, string(decision), forceApplied)
}

func buildDownloadDecisionResponse(taskID string, decision string, forceApplied bool) (int, map[string]any, error) {
	switch decision {
	case string(domain.ClaimDecisionReuseSuccess):
		return http.StatusOK, map[string]any{
			"ok":                 true,
			"duplicate":          true,
			"needs_confirmation": true,
			"task_id":            taskID,
			"download_url":       "/api/tasks/" + url.PathEscape(taskID) + "/download",
			"force_applied":      forceApplied,
		}, nil
	case string(domain.ClaimDecisionReuseActive):
		return http.StatusOK, map[string]any{
			"ok":            true,
			"duplicate":     true,
			"active":        true,
			"task_id":       taskID,
			"logs_url":      "/logs",
			"force_applied": forceApplied,
		}, nil
	case string(domain.ClaimDecisionCreated):
		return http.StatusAccepted, map[string]any{
			"ok":            true,
			"duplicate":     false,
			"active":        false,
			"task_id":       taskID,
			"logs_url":      "/logs",
			"force_applied": forceApplied,
		}, nil
	default:
		return 0, nil, fmt.Errorf("unknown download decision: %s", decision)
	}
}

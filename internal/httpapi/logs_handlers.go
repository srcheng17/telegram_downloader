package httpapi

import (
	"context"
	"net/http"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

func (a *API) handleSummary(w http.ResponseWriter, r *http.Request) {
	if a.taskCoreService != nil {
		summary, err := a.buildTaskCoreSummary(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}

	if a.legacyAdapter != nil && a.legacyAdapter.SupportsSummary() {
		summary, err := a.legacyAdapter.BuildSummary(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}

	summary, err := a.buildSummary(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (a *API) handleLogs(w http.ResponseWriter, r *http.Request) {
	queryValues := r.URL.Query()
	query := normalizeLogQuery(
		queryValues.Get("page"),
		queryValues.Get("per_page"),
		queryValues.Get("status"),
		queryValues.Get("q"),
	)

	if a.taskCoreService != nil {
		payload, err := a.readTaskCoreLogs(r.Context(), query, queryValues.Get("status"))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}

	if a.legacyAdapter != nil && a.legacyAdapter.SupportsLogs() {
		payload, err := a.legacyAdapter.ReadLogs(r.Context(), query)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}

	result, err := a.store.ListLogs(r.Context(), query)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	hasActiveTasks, err := a.store.HasActiveTasks(r.Context(), domain.ActiveTaskStatuses)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	summary, err := a.buildSummary(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	payload := domain.LogsResponse{
		Logs:           result.Logs,
		Total:          result.Total,
		Page:           result.Page,
		PerPage:        result.PerPage,
		TotalPages:     result.TotalPages,
		HasActiveTasks: hasActiveTasks,
		Filters: domain.LogFilters{
			Status: query.Status,
			Query:  query.Keyword,
		},
		StatusCatalog: domain.CopyStatusCatalog(),
		Summary:       summary,
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *API) buildSummary(ctx context.Context) (domain.Summary, error) {
	statusCounts, err := a.store.GetStatusCounts(ctx)
	if err != nil {
		return domain.Summary{}, err
	}

	return buildSummaryFromCounts(statusCounts, domain.DefaultStartupRecovery()), nil
}

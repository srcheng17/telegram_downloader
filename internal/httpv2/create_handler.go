package httpv2

import (
	"encoding/json"
	"net/http"
	"strings"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
)

func (h *TasksHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeError(w, http.StatusInternalServerError, "task handler dependencies are not configured")
		return
	}

	var request struct {
		URL          string  `json:"url"`
		CanonicalURL *string `json:"canonical_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	url := strings.TrimSpace(request.URL)
	if url == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	result, err := h.tasks.Create(r.Context(), apptasks.CreateInput{
		URL:          url,
		CanonicalURL: request.CanonicalURL,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create task")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"task_id": result.TaskID,
		"status":  result.Status,
	})
}

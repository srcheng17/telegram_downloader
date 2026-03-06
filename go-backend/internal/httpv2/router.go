package httpv2

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(store TaskStore, queue TaskQueue) http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r, NewTasksHandler(store, queue))
	return r
}

func RegisterRoutes(r chi.Router, h *TasksHandler) {
	r.Post("/v2/tasks", h.CreateTask)
	r.Get("/v2/tasks", h.ListTasks)
	r.Get("/v2/tasks/{task_id}", h.GetTask)
	r.Post("/v2/tasks/{task_id}/cancel", h.CancelTask)
	r.Get("/v2/tasks/{task_id}/artifact", h.DownloadArtifact)

	var settingsStore SettingsStore
	if h != nil {
		if typed, ok := any(h.store).(SettingsStore); ok {
			settingsStore = typed
		}
	}
	RegisterSettingsRoutes(r, NewSettingsHandler(settingsStore))
}

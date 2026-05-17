package httpapi

import (
	"fmt"
	"net/http"
)

func (a *API) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/healthz", http.StatusFound)
}

func (a *API) handleLogsPageRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/api/logs", http.StatusFound)
}

func (a *API) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "go-backend"})
}

func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if a.readyzChecker == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": true, "service": "go-backend"})
		return
	}

	ready, err := a.readyzChecker(r.Context())
	if err != nil {
		writeInternalError(w, fmt.Errorf("readyz check failed: %w", err))
		return
	}
	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"ready":   false,
			"reason":  "migrations_pending",
			"service": "go-backend",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": true, "service": "go-backend"})
}

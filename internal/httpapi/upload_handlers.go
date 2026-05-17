package httpapi

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func (a *API) handleMetadataHistory(w http.ResponseWriter, r *http.Request) {
	if a.uploadTaskStore == nil {
		writeInternalError(w, errors.New("metadata history store is not configured"))
		return
	}
	limit := 20
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			limit = parsed
		}
	}

	entries, err := a.uploadTaskStore.ListMetadataHistory(r.Context(), limit)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a *API) uploadTempDirOrDefault() string {
	if strings.TrimSpace(a.uploadTempDir) != "" {
		return strings.TrimSpace(a.uploadTempDir)
	}
	if tempPath := strings.TrimSpace(os.Getenv("TEMP_PATH")); tempPath != "" {
		return tempPath
	}
	return "temp_downloads"
}

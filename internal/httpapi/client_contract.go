package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ClientContractVersion is bumped when a mediactl write payload or response
// changes incompatibly. Older servers have no endpoint and fail closed.
const ClientContractVersion = 1

func RegisterClientContract(r chi.Router) {
	r.Get("/api/client-contract", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"protocol_version": ClientContractVersion,
			"client":           "mediactl",
		})
	})
}

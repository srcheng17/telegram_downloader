package httpv2

import "github.com/go-chi/chi/v5"

func RegisterSettings(r chi.Router, h *SettingsHandler) {
	RegisterSettingsRoutes(r, h)
}

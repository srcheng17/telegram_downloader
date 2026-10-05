package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
)

type TelegramAccountService interface {
	Account(context.Context) (app.Account, error)
	VerifyAccount(context.Context) error
	StartLogin(context.Context) (app.Snapshot, error)
	Attempt(context.Context, string) (app.Snapshot, error)
	Password(context.Context, string, string) error
	Cancel(context.Context, string) (app.Snapshot, error)
	SnapshotForTask(context.Context, string) (app.Input, error)
}
type Telegram struct{ service TelegramAccountService }

func NewTelegram(service TelegramAccountService) *Telegram { return &Telegram{service: service} }

// RegisterRoutes must be mounted behind the root administrator/CSRF middleware.
func (h *Telegram) RegisterRoutes(parent chi.Router) {
	parent.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if h.service == nil {
					telegramError(w, app.Failure("telegram_unavailable"))
					return
				}
				next.ServeHTTP(w, req)
			})
		})
		r.Get("/api/telegram/account", h.account)
		r.Post("/api/telegram/account/verify", h.verify)
		r.Post("/api/telegram/login-attempts", h.start)
		r.Get("/api/telegram/login-attempts/{id}", h.attempt)
		r.Get("/api/telegram/login-attempts/{id}/events", h.events)
		r.Post("/api/telegram/login-attempts/{id}/password", h.password)
		r.Post("/api/telegram/login-attempts/{id}/cancel", h.cancel)
	})
}
func telegramError(w http.ResponseWriter, err error) {
	code := "unavailable"
	status := http.StatusServiceUnavailable
	var failure *app.Error
	switch {
	case errors.Is(err, app.ErrBusy):
		code = "busy"
		status = 409
	case errors.Is(err, app.ErrNotFound):
		code = "not_found"
		status = 404
	case errors.Is(err, app.ErrConflict):
		code = "attempt_conflict"
		status = 409
	case errors.As(err, &failure):
		code = failure.Code
		switch code {
		case "invalid_input", "invalid_message_url", "invalid_source", "unsupported_media", "no_attachment", "ambiguous_media", "source_too_large", "source_size_unknown":
			status = 422
		case "auth_required", "account_changed":
			status = 409
		case "password_invalid":
			status = 422
		case "rate_limited":
			status = 429
		}
	}
	writeAPIErrorResponse(w, status, code, "Telegram 操作未完成，请根据当前状态重试。", nil)
}
func (h *Telegram) account(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.Account(r.Context())
	if e != nil {
		telegramError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Telegram) verify(w http.ResponseWriter, r *http.Request) {
	if e := h.service.VerifyAccount(r.Context()); e != nil {
		telegramError(w, e)
		return
	}
	h.account(w, r)
}
func (h *Telegram) start(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.StartLogin(r.Context())
	if e != nil {
		telegramError(w, e)
		return
	}
	writeJSON(w, 202, v)
}
func (h *Telegram) attempt(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.Attempt(r.Context(), chi.URLParam(r, "id"))
	if e != nil {
		telegramError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, v)
}
func (h *Telegram) password(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if strictJSON(w, r, &input) != nil {
		telegramError(w, app.Failure("invalid_input"))
		return
	}
	err := h.service.Password(r.Context(), chi.URLParam(r, "id"), input.Password)
	input.Password = ""
	if err != nil {
		telegramError(w, err)
		return
	}
	writeJSON(w, 202, map[string]bool{"accepted": true})
}
func (h *Telegram) cancel(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.Cancel(r.Context(), chi.URLParam(r, "id"))
	if e != nil {
		telegramError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Telegram) events(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	snapshot, err := h.service.Attempt(r.Context(), id)
	if err != nil {
		telegramError(w, err)
		return
	}
	controller := http.NewResponseController(w)
	if _, ok := w.(http.Flusher); !ok {
		telegramError(w, app.Failure("stream_unavailable"))
		return
	}
	if err = controller.SetWriteDeadline(time.Now().Add(20 * time.Second)); err != nil {
		telegramError(w, app.Failure("stream_unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(v app.Snapshot) error {
		if e := controller.SetWriteDeadline(time.Now().Add(20 * time.Second)); e != nil {
			return e
		}
		data, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", v.Seq, data); e != nil {
			return e
		}
		return controller.Flush()
	}
	if err = send(snapshot); err != nil {
		return
	}
	seq := snapshot.Seq
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		switch snapshot.State {
		case "connected", "cancelled", "expired", "failed":
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if controller.SetWriteDeadline(time.Now().Add(20*time.Second)) != nil {
				return
			}
			if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		case <-ticker.C:
			snapshot, err = h.service.Attempt(r.Context(), id)
			if err != nil {
				return
			}
			if snapshot.Seq > seq {
				if send(snapshot) != nil {
					return
				}
				seq = snapshot.Seq
			}
			if time.Now().After(snapshot.ExpiresAt) {
				return
			}
		}
	}
}

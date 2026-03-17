package httpui

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	appconfig "github.com/ryancheng/telegram-downloader/internal/config"
	webtemplates "github.com/ryancheng/telegram-downloader/web/templates"
)

type Handler struct {
	templates  map[string]*template.Template
	settings   Settings
	guardrails Guardrails
	store      SettingsStore
}

type pageData struct {
	Title       string
	CurrentPath string
	Settings    Settings
	Guardrails  Guardrails
}

type Settings struct {
	TaskConcurrency   int
	ImageConcurrency  int
	Timeout           int
	Retries           int
	LogRetentionDays  int
	FileRetentionDays int
}

type Guardrails struct {
	AllowedDomains []string
	MaxImages      int
	MaxImageBytes  int64
	MaxTotalBytes  int64
}

type Config struct {
	Settings      Settings
	Guardrails    Guardrails
	StaticDir     string
	SettingsStore SettingsStore
}

type SettingsStore interface {
	GetSettings(ctx context.Context) (appconfig.SettingsSnapshot, error)
	UpdateSettings(ctx context.Context, snapshot appconfig.SettingsSnapshot) (appconfig.SettingsSnapshot, error)
}

func NewRouter() http.Handler {
	return NewRouterWithConfig(Config{})
}

func NewRouterWithConfig(config Config) http.Handler {
	r := chi.NewRouter()
	RegisterRoutesWithConfig(r, config)
	return r
}

func RegisterRoutes(r chi.Router) {
	RegisterRoutesWithConfig(r, Config{})
}

func RegisterRoutesWithConfig(r chi.Router, config Config) {
	h := NewHandler(config)

	r.Get("/", h.Index)
	r.Get("/logs", h.Logs)
	r.Get("/settings", h.SettingsPage)
	r.Post("/settings", h.SaveSettings)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(resolveStaticDir(config.StaticDir)))))
}

func NewHandler(config Config) *Handler {
	return &Handler{
		templates: map[string]*template.Template{
			"index":    mustParseTemplate("templates/index.html"),
			"logs":     mustParseTemplate("templates/logs.html"),
			"settings": mustParseTemplate("templates/settings.html"),
		},
		settings:   normalizeSettings(config.Settings),
		guardrails: normalizeGuardrails(config.Guardrails),
		store:      config.SettingsStore,
	}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "index", pageData{
		Title:       "首页",
		CurrentPath: "/",
		Settings:    h.settings,
		Guardrails:  h.guardrails,
	})
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "logs", pageData{
		Title:       "日志",
		CurrentPath: "/logs",
		Settings:    h.settings,
		Guardrails:  h.guardrails,
	})
}

func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	settings := h.currentSettings(r)
	h.render(w, r, "settings", pageData{
		Title:       "设置",
		CurrentPath: "/settings",
		Settings:    settings,
		Guardrails:  h.guardrails,
	})
}

func (h *Handler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	if h.store != nil {
		snapshot, err := parseSettingsForm(r)
		if err != nil {
			http.Error(w, "invalid settings form", http.StatusBadRequest)
			return
		}
		if _, err := h.store.UpdateSettings(r.Context(), snapshot); err != nil {
			log.Printf("httpui update settings: %v", err)
			http.Error(w, "update settings", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (h *Handler) currentSettings(r *http.Request) Settings {
	settings := h.settings
	if h.store == nil {
		return settings
	}

	snapshot, err := h.store.GetSettings(r.Context())
	if err != nil {
		log.Printf("httpui get settings: %v", err)
		return settings
	}
	normalized := appconfig.NormalizeSettingsSnapshot(snapshot)
	settings.Timeout = normalized.Timeout
	settings.Retries = normalized.Retries
	settings.ImageConcurrency = normalized.ImageConcurrency
	return settings
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, templateName string, data pageData) {
	tpl, ok := h.templates[templateName]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	addVaryHeader(w.Header(), "HX-Request")
	layout := "base.html"
	if isHTMXRequest(r) {
		layout = "partial.html"
	}
	if err := tpl.ExecuteTemplate(w, layout, data); err != nil {
		log.Printf("httpui render template=%s: %v", templateName, err)
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}

func addVaryHeader(header http.Header, value string) {
	if header == nil {
		return
	}

	for _, existing := range header.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func isHTMXRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true")
}

func mustParseTemplate(page string) *template.Template {
	tpl, err := template.New("base.html").Funcs(template.FuncMap{
		"join": strings.Join,
		"formatGuardrailBytes": func(size int64) string {
			if size <= 0 {
				return "-"
			}
			return strconv.FormatInt(size, 10) + " 字节"
		},
	}).ParseFS(webtemplates.FS, "base.html", "partial.html", strings.TrimPrefix(page, "templates/"))
	if err != nil {
		panic(err)
	}
	return tpl
}

func resolveStaticDir(configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}

	candidates := []string{
		"web/static",
		filepath.Join("..", "..", "web", "static"),
		filepath.Join("..", "web", "static"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate
		}
	}
	return filepath.Join("..", "..", "web", "static")
}

func normalizeSettings(settings Settings) Settings {
	if settings.TaskConcurrency <= 0 {
		settings.TaskConcurrency = 2
	}
	if settings.ImageConcurrency <= 0 {
		settings.ImageConcurrency = 2
	}
	if settings.Timeout <= 0 {
		settings.Timeout = 30
	}
	if settings.Retries < 0 {
		settings.Retries = 10
	}
	if settings.LogRetentionDays <= 0 {
		settings.LogRetentionDays = 7
	}
	if settings.FileRetentionDays <= 0 {
		settings.FileRetentionDays = 7
	}
	return settings
}

func normalizeGuardrails(guardrails Guardrails) Guardrails {
	if len(guardrails.AllowedDomains) == 0 {
		guardrails.AllowedDomains = []string{"telegra.ph", "www.telegra.ph", "graph.org", "www.graph.org"}
	} else {
		guardrails.AllowedDomains = append([]string(nil), guardrails.AllowedDomains...)
	}
	if guardrails.MaxImages <= 0 {
		guardrails.MaxImages = 300
	}
	if guardrails.MaxImageBytes <= 0 {
		guardrails.MaxImageBytes = 25 * 1024 * 1024
	}
	if guardrails.MaxTotalBytes <= 0 {
		guardrails.MaxTotalBytes = 500 * 1024 * 1024
	}
	return guardrails
}

func parseSettingsForm(r *http.Request) (appconfig.SettingsSnapshot, error) {
	if r == nil {
		return appconfig.SettingsSnapshot{}, errors.New("request is nil")
	}
	if err := r.ParseForm(); err != nil {
		return appconfig.SettingsSnapshot{}, err
	}

	timeout, err := parseRequiredFormInt(r.FormValue("timeout"))
	if err != nil {
		return appconfig.SettingsSnapshot{}, err
	}
	retries, err := parseRequiredFormInt(r.FormValue("retries"))
	if err != nil {
		return appconfig.SettingsSnapshot{}, err
	}
	imageConcurrency, err := parseRequiredFormInt(r.FormValue("image_concurrency"))
	if err != nil {
		return appconfig.SettingsSnapshot{}, err
	}

	return appconfig.NormalizeSettingsSnapshot(appconfig.SettingsSnapshot{
		Timeout:          timeout,
		Retries:          retries,
		ImageConcurrency: imageConcurrency,
	}), nil
}

func parseRequiredFormInt(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, errors.New("missing value")
	}
	return strconv.Atoi(raw)
}

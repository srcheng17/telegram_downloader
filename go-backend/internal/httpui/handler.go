package httpui

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed templates/*.html
var templateFiles embed.FS

type Handler struct {
	templates  map[string]*template.Template
	settings   Settings
	guardrails Guardrails
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
	Settings   Settings
	Guardrails Guardrails
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
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(resolveStaticDir()))))
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
	}
}

func (h *Handler) Index(w http.ResponseWriter, _ *http.Request) {
	h.render(w, "index", pageData{
		Title:       "首页",
		CurrentPath: "/",
		Settings:    h.settings,
		Guardrails:  h.guardrails,
	})
}

func (h *Handler) Logs(w http.ResponseWriter, _ *http.Request) {
	h.render(w, "logs", pageData{
		Title:       "日志",
		CurrentPath: "/logs",
		Settings:    h.settings,
		Guardrails:  h.guardrails,
	})
}

func (h *Handler) SettingsPage(w http.ResponseWriter, _ *http.Request) {
	h.render(w, "settings", pageData{
		Title:       "设置",
		CurrentPath: "/settings",
		Settings:    h.settings,
		Guardrails:  h.guardrails,
	})
}

func (h *Handler) render(w http.ResponseWriter, templateName string, data pageData) {
	tpl, ok := h.templates[templateName]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "base.html", data); err != nil {
		log.Printf("httpui render template=%s: %v", templateName, err)
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}

func mustParseTemplate(page string) *template.Template {
	tpl, err := template.New("base.html").Funcs(template.FuncMap{
		"join": strings.Join,
		"formatGuardrailBytes": func(size int64) string {
			if size <= 0 {
				return "-"
			}
			const mb = int64(1024 * 1024)
			if size%mb == 0 {
				return fmt.Sprintf("%d MB", size/mb)
			}
			return fmt.Sprintf("%d 字节", size)
		},
	}).ParseFS(templateFiles, "templates/base.html", page)
	if err != nil {
		panic(err)
	}
	return tpl
}

func resolveStaticDir() string {
	candidates := []string{
		"static",
		filepath.Join("..", "static"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate
		}
	}
	return filepath.Join("..", "static")
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
	if settings.Retries == 0 {
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
		guardrails.AllowedDomains = []string{"telegra.ph", "graph.org"}
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

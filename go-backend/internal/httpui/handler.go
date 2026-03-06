package httpui

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

//go:embed templates/*.html
var templateFiles embed.FS

type Handler struct {
	templates map[string]*template.Template
}

type pageData struct {
	Title       string
	CurrentPath string
}

func NewRouter() http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r)
	return r
}

func RegisterRoutes(r chi.Router) {
	h := NewHandler()

	r.Get("/", h.Index)
	r.Get("/logs", h.Logs)
	r.Get("/settings", h.SettingsPage)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(resolveStaticDir()))))
}

func NewHandler() *Handler {
	return &Handler{
		templates: map[string]*template.Template{
			"index":    mustParseTemplate("templates/index.html"),
			"logs":     mustParseTemplate("templates/logs.html"),
			"settings": mustParseTemplate("templates/settings.html"),
		},
	}
}

func (h *Handler) Index(w http.ResponseWriter, _ *http.Request) {
	h.render(w, "index", pageData{
		Title:       "首页",
		CurrentPath: "/",
	})
}

func (h *Handler) Logs(w http.ResponseWriter, _ *http.Request) {
	h.render(w, "logs", pageData{
		Title:       "日志",
		CurrentPath: "/logs",
	})
}

func (h *Handler) SettingsPage(w http.ResponseWriter, _ *http.Request) {
	h.render(w, "settings", pageData{
		Title:       "设置",
		CurrentPath: "/settings",
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
	tpl, err := template.ParseFS(templateFiles, "templates/base.html", page)
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

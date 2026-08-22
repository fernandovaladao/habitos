package httpserver

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	webassets "habitos/web"
)

type Config struct {
	Port   string
	Logger *slog.Logger
}

type pageData struct {
	Title       string
	Description string
	Path        string
}

type handler struct {
	templates *template.Template
	staticFS  fs.FS
}

func New(config Config) (*http.Server, error) {
	if config.Port == "" {
		config.Port = "8080"
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	appHandler, err := NewHandler(config.Logger)
	if err != nil {
		return nil, err
	}

	return &http.Server{
		Addr:              ":" + config.Port,
		Handler:           appHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}, nil
}

func NewHandler(logger *slog.Logger) (http.Handler, error) {
	if logger == nil {
		logger = slog.Default()
	}

	templates, err := template.ParseFS(webassets.Files, "templates/layouts/*.html", "templates/pages/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("carregar templates: %w", err)
	}

	staticFS, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		return nil, fmt.Errorf("carregar assets: %w", err)
	}

	h := &handler{templates: templates, staticFS: staticFS}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /service-worker.js", h.serviceWorker)
	mux.HandleFunc("GET /static/manifest.webmanifest", h.manifest)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /{$}", h.home)

	pages := []pageData{
		{Title: "Criar Hábito", Description: "A criação de hábitos será disponibilizada em uma próxima etapa.", Path: "/criar-habito"},
		{Title: "Meus Hábitos", Description: "A gestão de hábitos será disponibilizada em uma próxima etapa.", Path: "/meus-habitos"},
		{Title: "Progresso", Description: "O acompanhamento de progresso será disponibilizado em uma próxima etapa.", Path: "/progresso"},
		{Title: "Recompensas", Description: "Recompensas e ranking serão disponibilizados em uma próxima etapa.", Path: "/recompensas"},
		{Title: "Aprenda os 4 Rs", Description: "O conteúdo completo sobre os 4 Rs será disponibilizado em uma próxima etapa.", Path: "/aprenda-4rs"},
		{Title: "Perfil", Description: "O perfil será disponibilizado em uma próxima etapa.", Path: "/perfil"},
	}
	for _, page := range pages {
		page := page
		mux.HandleFunc("GET "+page.Path, func(w http.ResponseWriter, r *http.Request) {
			h.render(w, http.StatusOK, "placeholder", page)
		})
	}

	return securityHeaders(requestLogger(logger, mux)), nil
}

func (h *handler) home(w http.ResponseWriter, _ *http.Request) {
	h.render(w, http.StatusOK, "home", pageData{
		Title:       "Início",
		Description: "Crie hábitos melhores, um passo de cada vez.",
		Path:        "/",
	})
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *handler) serviceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	http.ServeFileFS(w, r, h.staticFS, "service-worker.js")
}

func (h *handler) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	http.ServeFileFS(w, r, h.staticFS, "manifest.webmanifest")
}

func (h *handler) render(w http.ResponseWriter, status int, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("falha ao renderizar template", "template", name, "error", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("requisição HTTP", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

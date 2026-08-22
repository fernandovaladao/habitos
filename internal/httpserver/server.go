package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"habitos/internal/auth"
	"habitos/internal/csrf"
	"habitos/internal/profile"
	webassets "habitos/web"
)

type FirebaseWebConfig struct {
	APIKey          string `json:"apiKey"`
	AuthDomain      string `json:"authDomain"`
	ProjectID       string `json:"projectId"`
	AppID           string `json:"appId"`
	AuthEmulatorURL string `json:"authEmulatorUrl,omitempty"`
}

type Config struct {
	Port          string
	Logger        *slog.Logger
	SecureCookies bool
	FirebaseWeb   FirebaseWebConfig
}

type Dependencies struct {
	Sessions auth.SessionManager
	Profiles *profile.Service
}

type pageData struct {
	Title           string
	Description     string
	Path            string
	Authenticated   bool
	Email           string
	Profile         profile.Profile
	FirebaseEnabled bool
}

type handler struct {
	templates     *template.Template
	staticFS      fs.FS
	logger        *slog.Logger
	sessions      auth.SessionManager
	profiles      *profile.Service
	auth          *auth.Middleware
	csrf          *csrf.Protector
	secureCookies bool
	firebaseWeb   FirebaseWebConfig
}

func New(config Config, dependencies Dependencies) (*http.Server, error) {
	if config.Port == "" {
		config.Port = "8080"
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	appHandler, err := NewHandler(config, dependencies)
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

func NewHandler(config Config, dependencies Dependencies) (http.Handler, error) {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if dependencies.Sessions == nil || dependencies.Profiles == nil {
		return nil, errors.New("dependências de autenticação e perfil são obrigatórias")
	}

	templates, err := template.ParseFS(webassets.Files, "templates/layouts/*.html", "templates/pages/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("carregar templates: %w", err)
	}
	staticFS, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		return nil, fmt.Errorf("carregar assets: %w", err)
	}

	h := &handler{
		templates:     templates,
		staticFS:      staticFS,
		logger:        config.Logger,
		sessions:      dependencies.Sessions,
		profiles:      dependencies.Profiles,
		auth:          auth.NewMiddleware(dependencies.Sessions),
		csrf:          csrf.New(config.SecureCookies),
		secureCookies: config.SecureCookies,
		firebaseWeb:   config.FirebaseWeb,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /service-worker.js", h.serviceWorker)
	mux.HandleFunc("GET /static/manifest.webmanifest", h.manifest)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /api/firebase-config", h.firebaseConfig)
	mux.HandleFunc("GET /api/auth/csrf", h.issueCSRF)
	mux.Handle("POST /api/auth/session", h.csrf.Protect(http.HandlerFunc(h.createSession)))
	mux.Handle("POST /api/auth/logout", h.csrf.Protect(http.HandlerFunc(h.logout)))
	mux.Handle("POST /api/profile/ensure", h.auth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.ensureProfile))))
	mux.Handle("PUT /api/profile", h.auth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.updateProfile))))

	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /entrar", h.loginPage)
	mux.HandleFunc("GET /cadastro", h.signupPage)
	mux.HandleFunc("GET /recuperar-senha", h.passwordResetPage)
	mux.Handle("GET /alterar-senha", h.auth.RequirePage(http.HandlerFunc(h.changePasswordPage)))
	mux.Handle("GET /perfil", h.auth.RequirePage(http.HandlerFunc(h.profilePage)))
	mux.HandleFunc("GET /aprenda-4rs", h.publicPlaceholder(pageData{
		Title: "Aprenda os 4 Rs", Description: "O conteúdo completo sobre os 4 Rs será disponibilizado em uma próxima etapa.", Path: "/aprenda-4rs",
	}))

	privatePages := []pageData{
		{Title: "Criar Hábito", Description: "A criação de hábitos será disponibilizada em uma próxima etapa.", Path: "/criar-habito"},
		{Title: "Meus Hábitos", Description: "A gestão de hábitos será disponibilizada em uma próxima etapa.", Path: "/meus-habitos"},
		{Title: "Progresso", Description: "O acompanhamento de progresso será disponibilizado em uma próxima etapa.", Path: "/progresso"},
		{Title: "Recompensas", Description: "Recompensas e ranking serão disponibilizados em uma próxima etapa.", Path: "/recompensas"},
	}
	for _, page := range privatePages {
		page := page
		mux.Handle("GET "+page.Path, h.auth.RequirePage(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			page.Authenticated = true
			h.render(w, http.StatusOK, "placeholder", page)
		})))
	}

	return securityHeaders(requestLogger(config.Logger, mux), config.FirebaseWeb.AuthEmulatorURL != ""), nil
}

func (h *handler) home(w http.ResponseWriter, _ *http.Request) {
	h.render(w, http.StatusOK, "home", pageData{Title: "Início", Description: "Crie hábitos melhores, um passo de cada vez.", Path: "/"})
}

func (h *handler) loginPage(w http.ResponseWriter, _ *http.Request) {
	h.render(w, http.StatusOK, "login", pageData{Title: "Entrar", Description: "Entre na sua conta HÁBITOS.", FirebaseEnabled: true})
}

func (h *handler) signupPage(w http.ResponseWriter, _ *http.Request) {
	h.render(w, http.StatusOK, "signup", pageData{Title: "Criar conta", Description: "Crie sua conta HÁBITOS.", FirebaseEnabled: true})
}

func (h *handler) passwordResetPage(w http.ResponseWriter, _ *http.Request) {
	h.render(w, http.StatusOK, "password-reset", pageData{Title: "Recuperar senha", Description: "Receba por e-mail as instruções para recuperar sua senha.", FirebaseEnabled: true})
}

func (h *handler) changePasswordPage(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	h.render(w, http.StatusOK, "change-password", pageData{Title: "Alterar senha", Description: "Altere a senha da sua conta.", Authenticated: true, Email: identity.Email, FirebaseEnabled: true})
}

func (h *handler) profilePage(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	userProfile, err := h.profiles.EnsureProfile(r.Context(), identity, "UTC")
	if err != nil {
		h.logger.Error("falha ao carregar perfil", "error", err)
		h.render(w, http.StatusInternalServerError, "error", pageData{Title: "Erro", Description: "Não foi possível carregar seu perfil.", Authenticated: true})
		return
	}
	h.render(w, http.StatusOK, "profile", pageData{Title: "Perfil", Description: "Seu perfil básico.", Path: "/perfil", Authenticated: true, Email: identity.Email, Profile: userProfile})
}

func (h *handler) publicPlaceholder(page pageData) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		h.render(w, http.StatusOK, "placeholder", page)
	}
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) firebaseConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.firebaseWeb)
}

func (h *handler) issueCSRF(w http.ResponseWriter, _ *http.Request) {
	token, err := h.csrf.Issue(w)
	if err != nil {
		h.logger.Error("falha ao gerar token CSRF", "error", err)
		http.Error(w, "Não foi possível iniciar a operação.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"csrfToken": token})
}

func (h *handler) createSession(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IDToken string `json:"idToken"`
	}
	if err := decodeJSON(r, &input); err != nil || input.IDToken == "" {
		http.Error(w, "Dados de autenticação inválidos.", http.StatusBadRequest)
		return
	}
	sessionCookie, err := h.sessions.CreateSession(r.Context(), input.IDToken, auth.SessionDuration)
	if err != nil {
		http.Error(w, "Não foi possível autenticar.", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionCookie,
		Path:     "/",
		MaxAge:   int(auth.SessionDuration.Seconds()),
		Expires:  time.Now().Add(auth.SessionDuration),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "authenticated"})
}

func (h *handler) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	h.csrf.Clear(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed_out"})
}

func (h *handler) ensureProfile(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Timezone string `json:"timezone"`
	}
	if err := decodeJSON(r, &input); err != nil {
		http.Error(w, "Dados de perfil inválidos.", http.StatusBadRequest)
		return
	}
	userProfile, err := h.profiles.EnsureProfile(r.Context(), identity, input.Timezone)
	if err != nil {
		h.logger.Error("falha ao garantir perfil", "error", err)
		http.Error(w, "Não foi possível preparar o perfil.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, userProfile)
}

func (h *handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Nickname     string `json:"nickname"`
		Age          int    `json:"age"`
		Timezone     string `json:"timezone"`
		RankingOptIn bool   `json:"rankingOptIn"`
	}
	if err := decodeJSON(r, &input); err != nil {
		http.Error(w, "Dados de perfil inválidos.", http.StatusBadRequest)
		return
	}
	userProfile, err := h.profiles.Update(r.Context(), identity, profile.Update{
		Nickname: input.Nickname, Age: input.Age, Timezone: input.Timezone, RankingOptIn: input.RankingOptIn,
	})
	if errors.Is(err, profile.ErrInvalidNickname) || errors.Is(err, profile.ErrInvalidAge) || errors.Is(err, profile.ErrInvalidTimezone) {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		h.logger.Error("falha ao atualizar perfil", "error", err)
		http.Error(w, "Não foi possível atualizar o perfil.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, userProfile)
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
	var output bytes.Buffer
	if err := h.templates.ExecuteTemplate(&output, name, data); err != nil {
		h.logger.Error("falha ao renderizar template", "template", name, "error", err)
		http.Error(w, "Não foi possível carregar a página.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = output.WriteTo(w)
}

func decodeJSON(r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler, allowLocalEmulator bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectSources := "'self' https://identitytoolkit.googleapis.com https://securetoken.googleapis.com"
		if allowLocalEmulator {
			connectSources += " http://127.0.0.1:* http://localhost:*"
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self' https://www.gstatic.com; connect-src "+connectSources+"; manifest-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
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

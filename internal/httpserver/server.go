package httpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"habitos/internal/accountdeletion"
	"habitos/internal/accountstate"
	"habitos/internal/auth"
	"habitos/internal/avatar"
	"habitos/internal/csrf"
	"habitos/internal/execution"
	"habitos/internal/gamification"
	"habitos/internal/habit"
	"habitos/internal/habitsuggestion"
	"habitos/internal/note"
	"habitos/internal/profile"
	"habitos/internal/progress"
	"habitos/internal/ranking"
	"habitos/internal/reminder"
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
	Port              string
	Logger            *slog.Logger
	SecureCookies     bool
	FirebaseWeb       FirebaseWebConfig
	VAPIDPublicKey    string
	ReminderProcessor bool
	Production        bool
	WriteTimeout      time.Duration
}

type Dependencies struct {
	Sessions     auth.SessionManager
	Profiles     *profile.Service
	Habits       *habit.Service
	Executions   *execution.Service
	Notes        *note.Service
	Progress     *progress.Service
	Ranking      *ranking.Service
	Suggestions  *habitsuggestion.Service
	Avatars      *avatar.Service
	Deletion     AccountDeletion
	AccountState accountstate.Checker
	Reminders    ReminderService
}

type ReminderService interface {
	Reconcile(context.Context, auth.Identity, bool) error
	RegisterSubscription(context.Context, auth.Identity, string, string, string) (reminder.Subscription, error)
	DisableSubscription(context.Context, auth.Identity, string) error
	Subscriptions(context.Context, auth.Identity) ([]reminder.Subscription, error)
	Process(context.Context) (int, error)
}

type AccountDeletion interface {
	Start(context.Context, auth.Identity, string, string) (accountdeletion.Result, error)
	Continue(context.Context, auth.Identity) (accountdeletion.Result, error)
}

type pageData struct {
	Title             string
	Description       string
	Path              string
	Authenticated     bool
	Email             string
	Profile           profile.Profile
	Habit             habit.Habit
	Habits            []habit.Habit
	Filter            habit.ListFilter
	SchedulePending   bool
	Execution         *execution.Execution
	Executions        []execution.Execution
	Notes             []note.Note
	TodayExecutions   map[string]*execution.Execution
	WeeklyProgress    progress.WeeklySummary
	TodayProgress     progress.Rate
	HasTodayProgress  bool
	NextHistoryBefore string
	Streak            gamification.Streak
	Streaks           []gamification.Streak
	Achievements      []gamification.UserAchievement
	MaxCurrentStreak  int
	HabitTitles       map[string]string
	Progress          progress.Report
	Ranking           ranking.Board
	ProfileRank       *ranking.PublicEntry
	FirebaseEnabled   bool
	VAPIDPublicKey    string
	PushSubscriptions []reminder.Subscription
}

type handler struct {
	templates         *template.Template
	staticFS          fs.FS
	logger            *slog.Logger
	sessions          auth.SessionManager
	profiles          *profile.Service
	habits            *habit.Service
	executions        *execution.Service
	notes             *note.Service
	progress          *progress.Service
	ranking           *ranking.Service
	suggestions       *habitsuggestion.Service
	avatars           *avatar.Service
	deletion          AccountDeletion
	reminders         ReminderService
	auth              *auth.Middleware
	activeAuth        *auth.Middleware
	csrf              *csrf.Protector
	secureCookies     bool
	firebaseWeb       FirebaseWebConfig
	vapidPublicKey    string
	reminderProcessor bool
	production        bool
	limiters          *limiterRegistry
}

func New(config Config, dependencies Dependencies) (*http.Server, error) {
	if config.Port == "" {
		config.Port = "8080"
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 30 * time.Second
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
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       60 * time.Second,
	}, nil
}

func NewHandler(config Config, dependencies Dependencies) (http.Handler, error) {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if dependencies.Sessions == nil || dependencies.Profiles == nil || dependencies.Habits == nil || dependencies.Executions == nil || dependencies.Notes == nil || dependencies.Progress == nil || dependencies.Ranking == nil || dependencies.Suggestions == nil || dependencies.Avatars == nil || dependencies.Deletion == nil || dependencies.AccountState == nil || dependencies.Reminders == nil {
		return nil, errors.New("dependências de autenticação, perfil, hábitos, execuções, notas, progresso, ranking, sugestões e avatares são obrigatórias")
	}

	templates, err := template.New("").Funcs(template.FuncMap{"amount": formatHundredths, "unitLabel": unitLabel, "weekdayLabel": weekdayLabel, "statusLabel": statusLabel, "executionStatusLabel": executionStatusLabel, "reminderLabel": reminderLabel, "ratePercent": ratePercent, "inversePercent": func(value progress.Rate) int { return 100 - ratePercent(value) }, "countPercent": countPercent, "shortDate": shortDate, "hasWeekday": hasWeekday, "listWeekdays": func() []int { return []int{1, 2, 3, 4, 5, 6, 7} }}).ParseFS(webassets.Files, "templates/layouts/*.html", "templates/pages/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("carregar templates: %w", err)
	}
	staticFS, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		return nil, fmt.Errorf("carregar assets: %w", err)
	}

	h := &handler{
		templates:         templates,
		staticFS:          staticFS,
		logger:            config.Logger,
		sessions:          dependencies.Sessions,
		profiles:          dependencies.Profiles,
		habits:            dependencies.Habits,
		executions:        dependencies.Executions,
		notes:             dependencies.Notes,
		progress:          dependencies.Progress,
		ranking:           dependencies.Ranking,
		suggestions:       dependencies.Suggestions,
		avatars:           dependencies.Avatars,
		deletion:          dependencies.Deletion,
		reminders:         dependencies.Reminders,
		auth:              auth.NewMiddleware(dependencies.Sessions),
		activeAuth:        auth.NewActiveMiddleware(dependencies.Sessions, dependencies.AccountState),
		csrf:              csrf.New(config.SecureCookies),
		secureCookies:     config.SecureCookies,
		firebaseWeb:       config.FirebaseWeb,
		vapidPublicKey:    config.VAPIDPublicKey,
		reminderProcessor: config.ReminderProcessor,
		production:        config.Production,
		limiters:          newLimiterRegistry(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /service-worker.js", h.serviceWorker)
	mux.HandleFunc("GET /static/manifest.webmanifest", h.manifest)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /api/firebase-config", h.firebaseConfig)
	mux.HandleFunc("GET /api/auth/csrf", h.issueCSRF)
	mux.Handle("POST /api/auth/session", h.rateLimit("session", 10, time.Minute, h.csrf.Protect(http.HandlerFunc(h.createSession))))
	mux.Handle("POST /api/auth/logout", h.csrf.Protect(http.HandlerFunc(h.logout)))
	mux.Handle("POST /api/account/deletion/start", h.auth.RequireAPI(h.rateLimit("account-deletion-start", 5, time.Minute, h.csrf.Protect(http.HandlerFunc(h.startAccountDeletion)))))
	mux.Handle("POST /api/account/deletion/continue", h.auth.RequireAPI(h.rateLimit("account-deletion-continue", 60, time.Minute, h.csrf.Protect(http.HandlerFunc(h.continueAccountDeletion)))))
	mux.Handle("POST /api/profile/ensure", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.ensureProfile))))
	mux.Handle("PUT /api/profile", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.updateProfile))))
	mux.Handle("POST /api/profile/photo", h.activeAuth.RequireAPI(h.rateLimit("photo", 6, time.Minute, h.csrf.Protect(http.HandlerFunc(h.uploadProfilePhoto)))))
	mux.Handle("DELETE /api/profile/photo", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.removeProfilePhoto))))
	mux.Handle("PUT /api/profile/avatar/internal", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.selectInternalAvatar))))
	mux.Handle("GET /media/avatars/{id}", h.activeAuth.RequireAPI(http.HandlerFunc(h.profilePhoto)))
	mux.Handle("POST /api/habits", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.createHabit))))
	mux.Handle("POST /api/habit-suggestions", h.activeAuth.RequireAPI(h.rateLimit("ai-suggestion", 10, time.Minute, h.csrf.Protect(http.HandlerFunc(h.suggestHabit)))))
	mux.Handle("PUT /api/habits/{id}", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.updateHabit))))
	mux.Handle("POST /api/habits/{id}/duplicate", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.duplicateHabit))))
	mux.Handle("POST /api/habits/{id}/archive", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.archiveHabit))))
	mux.Handle("POST /api/habits/{id}/reactivate", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.reactivateHabit))))
	mux.Handle("DELETE /api/habits/{id}", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.deleteHabit))))
	mux.Handle("GET /api/reminders/subscriptions", h.activeAuth.RequireAPI(http.HandlerFunc(h.listPushSubscriptions)))
	mux.Handle("POST /api/reminders/subscriptions", h.activeAuth.RequireAPI(h.rateLimit("push-subscription", 20, time.Minute, h.csrf.Protect(http.HandlerFunc(h.registerPushSubscription)))))
	mux.Handle("DELETE /api/reminders/subscriptions/{id}", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.disablePushSubscription))))
	if h.reminderProcessor {
		mux.HandleFunc("POST /internal/reminders/process", h.processReminders)
	}
	mux.Handle("POST /api/executions/{id}/simple", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.recordSimple))))
	mux.Handle("POST /api/executions/{id}/quantitative", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.recordQuantitative))))
	mux.Handle("POST /api/habits/{id}/notes", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.createNote))))
	mux.Handle("PUT /api/notes/{id}", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.updateNote))))
	mux.Handle("DELETE /api/notes/{id}", h.activeAuth.RequireAPI(h.csrf.Protect(http.HandlerFunc(h.deleteNote))))

	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /entrar", h.loginPage)
	mux.HandleFunc("GET /cadastro", h.signupPage)
	mux.HandleFunc("GET /recuperar-senha", h.passwordResetPage)
	mux.Handle("GET /exclusao-conta", h.auth.RequirePage(http.HandlerFunc(h.accountDeletionPage)))
	mux.Handle("GET /alterar-senha", h.activeAuth.RequirePage(http.HandlerFunc(h.changePasswordPage)))
	mux.Handle("GET /perfil", h.activeAuth.RequirePage(http.HandlerFunc(h.profilePage)))
	mux.Handle("GET /criar-habito", h.activeAuth.RequirePage(http.HandlerFunc(h.createHabitPage)))
	mux.Handle("GET /meus-habitos", h.activeAuth.RequirePage(http.HandlerFunc(h.habitsPage)))
	mux.Handle("GET /habitos/{id}", h.activeAuth.RequirePage(http.HandlerFunc(h.habitDetailsPage)))
	mux.Handle("GET /habitos/{id}/editar", h.activeAuth.RequirePage(http.HandlerFunc(h.editHabitPage)))
	mux.Handle("GET /progresso", h.activeAuth.RequirePage(http.HandlerFunc(h.progressPage)))
	mux.Handle("GET /recompensas", h.activeAuth.RequirePage(http.HandlerFunc(h.rewardsPage)))
	mux.HandleFunc("GET /aprenda-4rs", h.learnFourRsPage)

	return requestContext(requestLogger(config.Logger, securityHeaders(mux, config.FirebaseWeb.AuthEmulatorURL != "", config.Production))), nil
}

func (h *handler) home(w http.ResponseWriter, _ *http.Request) {
	h.render(w, http.StatusOK, "home", pageData{Title: "Início", Description: "Crie hábitos melhores, um passo de cada vez.", Path: "/"})
}

func (h *handler) learnFourRsPage(w http.ResponseWriter, _ *http.Request) {
	h.render(w, http.StatusOK, "learn-four-rs", pageData{Title: "Aprenda os 4 Rs", Description: "Entenda o ciclo de formação e manutenção de hábitos.", Path: "/aprenda-4rs"})
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
	var profileRank *ranking.PublicEntry
	if userProfile.RankingOptIn && userProfile.ProfileComplete {
		position, err := h.ranking.Position(r.Context(), identity)
		if err != nil {
			h.logger.Error("falha ao carregar posição do perfil", "error", err)
			h.render(w, http.StatusInternalServerError, "error", pageData{Title: "Erro", Description: "Não foi possível carregar seu perfil.", Authenticated: true})
			return
		}
		profileRank = &position
	}
	subscriptions, err := h.reminders.Subscriptions(r.Context(), identity)
	if err != nil {
		h.logger.Error("falha ao carregar dispositivos de notificação")
		subscriptions = nil
	}
	h.render(w, http.StatusOK, "profile", pageData{Title: "Perfil", Description: "Seu perfil e preferências.", Path: "/perfil", Authenticated: true, Email: identity.Email, Profile: userProfile, ProfileRank: profileRank, VAPIDPublicKey: h.vapidPublicKey, PushSubscriptions: subscriptions})
}

func (h *handler) authenticatedProfile(r *http.Request) (auth.Identity, profile.Profile, error) {
	identity, _ := auth.IdentityFromContext(r.Context())
	value, err := h.profiles.EnsureProfile(r.Context(), identity, "UTC")
	return identity, value, err
}

func (h *handler) createHabitPage(w http.ResponseWriter, r *http.Request) {
	_, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	h.render(w, http.StatusOK, "habit-form", pageData{Title: "Criar Hábito", Description: "Crie um hábito e configure sua rotina.", Path: "/criar-habito", Authenticated: true, Profile: userProfile})
}

func (h *handler) editHabitPage(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	value, err := h.habits.Get(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	h.render(w, http.StatusOK, "habit-form", pageData{Title: "Editar Hábito", Description: "Atualize a configuração do hábito.", Path: "/meus-habitos", Authenticated: true, Profile: userProfile, Habit: value})
}

func (h *handler) habitsPage(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	filter := habit.ListFilter(r.URL.Query().Get("filtro"))
	if filter == "" {
		filter = habit.FilterAll
	}
	if filter != habit.FilterAll && filter != habit.FilterToday && filter != habit.FilterCompleted {
		filter = habit.FilterAll
	}
	// The weekly summary must not depend on the visual filter. Synchronize every
	// non-deleted habit first, then derive Today/Completed from the resulting
	// authoritative executions returned by the single weekly query.
	values, err := h.habits.List(r.Context(), identity, userProfile.Timezone, habit.FilterAll)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	for _, value := range values {
		if err := h.syncHabit(r, identity, userProfile, value); err != nil {
			h.renderHabitError(w, err)
			return
		}
	}
	weeklyProgress, err := h.progress.WeekSummary(r.Context(), identity, userProfile.Timezone)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	todayExecutions := make(map[string]*execution.Execution, len(weeklyProgress.TodayByHabit))
	for habitID, item := range weeklyProgress.TodayByHabit {
		copy := item
		todayExecutions[habitID] = &copy
	}
	if filter == habit.FilterToday || filter == habit.FilterCompleted {
		filtered := values[:0]
		for _, value := range values {
			item, ok := todayExecutions[value.ID]
			if !ok || value.Status != habit.StatusActive {
				continue
			}
			if filter == habit.FilterToday || item.Status == execution.StatusCompleted {
				filtered = append(filtered, value)
			}
		}
		values = filtered
	}
	h.render(w, http.StatusOK, "habits", pageData{Title: "Meus Hábitos", Description: "Gerencie seus hábitos.", Path: "/meus-habitos", Authenticated: true, Profile: userProfile, Habits: values, Filter: filter, TodayExecutions: todayExecutions, WeeklyProgress: weeklyProgress})
}

func (h *handler) habitDetailsPage(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	value, err := h.habits.Get(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	if err := h.syncHabit(r, identity, userProfile, value); err != nil {
		h.renderHabitError(w, err)
		return
	}
	latest, err := h.executions.History(r.Context(), identity, value.ID, "")
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	history := latest
	if before := r.URL.Query().Get("before"); before != "" {
		history, err = h.executions.History(r.Context(), identity, value.ID, before)
		if err != nil {
			h.renderHabitError(w, err)
			return
		}
	}
	notes, err := h.notes.List(r.Context(), identity, value.ID)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	var current *execution.Execution
	today := todayIn(userProfile.Timezone)
	for index := range latest {
		if latest[index].ScheduledDate == today {
			copy := latest[index]
			current = &copy
			break
		}
	}
	nextBefore := ""
	if len(history) == 30 {
		nextBefore = history[len(history)-1].ScheduledDate
	}
	streak, err := h.executions.Streak(r.Context(), identity, value.ID)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	weeklyProgress, err := h.progress.WeekSummary(r.Context(), identity, userProfile.Timezone)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	todayProgress := progress.EmptyRate()
	hasTodayProgress := false
	if current != nil {
		contribution, resolved, progressErr := progress.ExecutionContribution(*current)
		if progressErr != nil {
			h.renderHabitError(w, progressErr)
			return
		}
		if resolved {
			todayProgress = progress.Rate{Contribution: contribution, Denominator: 1}
			hasTodayProgress = true
		}
	}
	h.render(w, http.StatusOK, "habit-details", pageData{Title: "Detalhes do Hábito", Description: value.Title, Path: "/meus-habitos", Authenticated: true, Profile: userProfile, Habit: value, SchedulePending: value.PreviousSchedule != nil && today < value.ScheduleEffectiveDate, Execution: current, Executions: history, Notes: notes, NextHistoryBefore: nextBefore, Streak: streak, WeeklyProgress: weeklyProgress, TodayProgress: todayProgress, HasTodayProgress: hasTodayProgress})
}

func (h *handler) progressData(r *http.Request) (pageData, error) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		return pageData{}, err
	}
	habits, err := h.habits.List(r.Context(), identity, userProfile.Timezone, habit.FilterAll)
	if err != nil {
		return pageData{}, err
	}
	var streaks []gamification.Streak
	habitTitles := make(map[string]string, len(habits))
	maxCurrent := 0
	for _, value := range habits {
		habitTitles[value.ID] = value.Title
		if err := h.syncHabit(r, identity, userProfile, value); err != nil {
			return pageData{}, err
		}
		streak, err := h.executions.Streak(r.Context(), identity, value.ID)
		if err != nil {
			return pageData{}, err
		}
		streaks = append(streaks, streak)
		if value.Status == habit.StatusActive && streak.CurrentStreak > maxCurrent {
			maxCurrent = streak.CurrentStreak
		}
	}
	userProfile, err = h.profiles.Get(r.Context(), identity)
	if err != nil {
		return pageData{}, err
	}
	achievements, err := h.executions.Achievements(r.Context(), identity)
	if err != nil {
		return pageData{}, err
	}
	return pageData{Authenticated: true, Profile: userProfile, Habits: habits, Streaks: streaks, Achievements: achievements, MaxCurrentStreak: maxCurrent, HabitTitles: habitTitles}, nil
}

func (h *handler) progressPage(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	habits, err := h.habits.List(r.Context(), identity, userProfile.Timezone, habit.FilterAll)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	for _, value := range habits {
		if err := h.syncHabit(r, identity, userProfile, value); err != nil {
			h.renderHabitError(w, err)
			return
		}
	}
	report, err := h.progress.Report(r.Context(), identity, userProfile.Timezone, progress.Query{Kind: progress.PeriodKind(r.URL.Query().Get("periodo")), StartDate: r.URL.Query().Get("inicio"), EndDate: r.URL.Query().Get("fim")})
	if errors.Is(err, progress.ErrInvalidPeriod) || errors.Is(err, progress.ErrPeriodTooLong) {
		h.render(w, http.StatusUnprocessableEntity, "error", pageData{Title: "Período inválido", Description: err.Error(), Path: "/progresso", Authenticated: true, Profile: userProfile})
		return
	}
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	h.render(w, http.StatusOK, "progress", pageData{Title: "Progresso", Description: "Acompanhe seu progresso no período.", Path: "/progresso", Authenticated: true, Profile: userProfile, Progress: report})
}

func (h *handler) rewardsPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.progressData(r)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	identity, _ := auth.IdentityFromContext(r.Context())
	participating := data.Profile.RankingOptIn && data.Profile.ProfileComplete && profile.ValidateNickname(data.Profile.Nickname) == nil
	board, err := h.ranking.Board(r.Context(), identity, participating)
	if err != nil {
		h.renderHabitError(w, err)
		return
	}
	data.Ranking = board
	data.Title, data.Description, data.Path = "Recompensas", "Veja seus bônus e conquistas.", "/recompensas"
	h.render(w, http.StatusOK, "rewards", data)
}

func (h *handler) syncHabit(r *http.Request, identity auth.Identity, userProfile profile.Profile, value habit.Habit) error {
	versions, err := h.habits.ScheduleVersions(r.Context(), identity, value.ID)
	if err != nil {
		return err
	}
	return h.executions.SyncHabit(r.Context(), identity, value, versions, userProfile.Timezone, todayIn(userProfile.Timezone))
}
func todayIn(timezone string) string {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Now().UTC().Format("2006-01-02")
	}
	return time.Now().In(location).Format("2006-01-02")
}

func (h *handler) renderHabitError(w http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	description := "Não foi possível concluir a operação."
	if errors.Is(err, habit.ErrNotFound) {
		statusCode = http.StatusNotFound
		description = "Hábito não encontrado."
	}
	h.render(w, statusCode, "error", pageData{Title: "Erro", Description: description, Authenticated: true})
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
	if err := decodeJSON(w, r, &input); err != nil || input.IDToken == "" {
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
	h.clearSession(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed_out"})
}

func (h *handler) clearSession(w http.ResponseWriter) {
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
}

func (h *handler) startAccountDeletion(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Confirmation string `json:"confirmation"`
		IDToken      string `json:"idToken"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "Confirmação inválida.", http.StatusBadRequest)
		return
	}
	result, err := h.deletion.Start(r.Context(), identity, input.Confirmation, input.IDToken)
	if errors.Is(err, accountdeletion.ErrInvalidConfirmation) {
		http.Error(w, "Digite EXCLUIR MINHA CONTA para confirmar.", http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, auth.ErrInvalidSession) || errors.Is(err, accountdeletion.ErrIdentityMismatch) {
		http.Error(w, "Autenticação recente necessária.", http.StatusUnauthorized)
		return
	}
	h.writeDeletionResult(w, result, err)
}

func (h *handler) continueAccountDeletion(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	result, err := h.deletion.Continue(r.Context(), identity)
	h.writeDeletionResult(w, result, err)
}

func (h *handler) writeDeletionResult(w http.ResponseWriter, result accountdeletion.Result, err error) {
	if err != nil {
		h.logger.Error("falha na exclusão da conta", "stage", result.Stage, "code", "deletion_step_failed")
		http.Error(w, "Não foi possível continuar a exclusão agora.", http.StatusServiceUnavailable)
		return
	}
	if result.Complete {
		h.clearSession(w)
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (h *handler) accountDeletionPage(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	h.render(w, http.StatusOK, "account-deletion", pageData{Title: "Exclusão da conta", Description: "Sua exclusão está em andamento.", Authenticated: true, Email: identity.Email, FirebaseEnabled: true})
}

func (h *handler) ensureProfile(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Timezone string `json:"timezone"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
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
	currentProfile, profileErr := h.profiles.Get(r.Context(), identity)
	if profileErr != nil {
		h.writeHabitError(w, profileErr)
		return
	}
	var input struct {
		Nickname                    string `json:"nickname"`
		Age                         int    `json:"age"`
		Timezone                    string `json:"timezone"`
		RankingOptIn                bool   `json:"rankingOptIn"`
		Weight                      string `json:"weight"`
		Height                      string `json:"height"`
		Gender                      string `json:"gender"`
		ReminderNotificationEnabled *bool  `json:"reminderNotificationEnabled"`
		ReminderEmailEnabled        *bool  `json:"reminderEmailEnabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		http.Error(w, "Dados de perfil inválidos.", http.StatusBadRequest)
		return
	}
	weight, weightErr := parsePositiveOptionalHundredths(input.Weight)
	height, heightErr := parsePositiveOptionalHundredths(input.Height)
	if weightErr != nil || heightErr != nil {
		http.Error(w, "Peso e altura devem ser positivos e ter até 2 casas decimais.", http.StatusUnprocessableEntity)
		return
	}
	if input.Timezone != currentProfile.Timezone {
		values, err := h.habits.List(r.Context(), identity, currentProfile.Timezone, habit.FilterAll)
		if err != nil {
			h.writeHabitError(w, err)
			return
		}
		for _, value := range values {
			if err := h.syncHabit(r, identity, currentProfile, value); err != nil {
				h.writeHabitError(w, err)
				return
			}
		}
	}
	reminderNotificationEnabled := currentProfile.ReminderNotificationEnabled
	if input.ReminderNotificationEnabled != nil {
		reminderNotificationEnabled = *input.ReminderNotificationEnabled
	}
	reminderEmailEnabled := currentProfile.ReminderEmailEnabled
	if input.ReminderEmailEnabled != nil {
		reminderEmailEnabled = *input.ReminderEmailEnabled
	}
	userProfile, err := h.profiles.Update(r.Context(), identity, profile.Update{
		Nickname: input.Nickname, Age: input.Age, Timezone: input.Timezone, RankingOptIn: input.RankingOptIn, WeightHundredths: weight, HeightHundredths: height, Gender: input.Gender,
		AvatarType: currentProfile.AvatarType, ReminderNotificationEnabled: reminderNotificationEnabled, ReminderEmailEnabled: reminderEmailEnabled,
	})
	if errors.Is(err, profile.ErrInvalidNickname) || errors.Is(err, profile.ErrInvalidAge) || errors.Is(err, profile.ErrInvalidTimezone) || errors.Is(err, profile.ErrInvalidWeight) || errors.Is(err, profile.ErrInvalidHeight) || errors.Is(err, profile.ErrInvalidGender) || errors.Is(err, profile.ErrInvalidAvatar) {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		h.logger.Error("falha ao atualizar perfil", "error", err)
		http.Error(w, "Não foi possível atualizar o perfil.", http.StatusInternalServerError)
		return
	}
	if err := h.reminders.Reconcile(r.Context(), identity, input.Timezone != currentProfile.Timezone); err != nil {
		h.logger.Error("falha ao reconciliar lembretes do perfil")
		http.Error(w, "Perfil salvo, mas não foi possível atualizar os lembretes.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, userProfile)
}

func (h *handler) uploadProfilePhoto(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, avatar.MaxUploadBytes+1024*1024)
	file, _, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, "Selecione uma imagem válida.", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	_, err = h.avatars.Upload(r.Context(), identity, file)
	if errors.Is(err, avatar.ErrTooLarge) {
		http.Error(w, "A imagem excede os limites permitidos.", http.StatusRequestEntityTooLarge)
		return
	}
	if errors.Is(err, avatar.ErrInvalidImage) {
		http.Error(w, "A imagem deve ser JPEG, PNG ou WebP válido.", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		h.logger.Error("falha ao atualizar foto privada")
		http.Error(w, "Não foi possível atualizar a foto.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *handler) removeProfilePhoto(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	if err := h.avatars.RemovePhoto(r.Context(), identity); err != nil {
		h.logger.Error("falha ao remover foto privada")
		http.Error(w, "Não foi possível remover a foto.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *handler) selectInternalAvatar(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		AvatarType string `json:"avatarType"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "Avatar inválido.", http.StatusBadRequest)
		return
	}
	if err := h.avatars.SelectInternal(r.Context(), identity, input.AvatarType); errors.Is(err, profile.ErrInvalidAvatar) {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	} else if err != nil {
		h.logger.Error("falha ao selecionar avatar interno")
		http.Error(w, "Não foi possível atualizar o avatar.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatarType": input.AvatarType})
}

func (h *handler) profilePhoto(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	id := r.PathValue("id")
	content, err := h.avatars.Read(r.Context(), identity, id)
	if err != nil {
		http.Error(w, "Foto não encontrada.", http.StatusNotFound)
		return
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(content))
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", `inline; filename="foto-perfil.jpg"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

type habitRequest struct {
	Title                  string                `json:"title"`
	Description            string                `json:"description"`
	GoalType               habit.GoalType        `json:"goalType"`
	Target                 string                `json:"target"`
	Unit                   habit.Unit            `json:"unit"`
	CustomUnit             string                `json:"customUnit"`
	Weekdays               []int                 `json:"weekdays"`
	Time                   string                `json:"time"`
	WeeklyTargetExecutions int                   `json:"weeklyTargetExecutions"`
	Reminder               habit.ReminderChannel `json:"reminder"`
	Age                    int                   `json:"age"`
	Weight                 string                `json:"weight"`
	Height                 string                `json:"height"`
	Gender                 string                `json:"gender"`
}

func (input habitRequest) domain() (habit.Input, profile.Demographics, error) {
	target, err := parseOptionalHundredths(input.Target)
	if err != nil {
		return habit.Input{}, profile.Demographics{}, habit.ErrInvalidGoal
	}
	weight, err := parsePositiveOptionalHundredths(input.Weight)
	if err != nil {
		return habit.Input{}, profile.Demographics{}, profile.ErrInvalidWeight
	}
	height, err := parsePositiveOptionalHundredths(input.Height)
	if err != nil {
		return habit.Input{}, profile.Demographics{}, profile.ErrInvalidHeight
	}
	value := habit.Input{Title: input.Title, Description: input.Description, GoalType: input.GoalType, TargetHundredths: target, Unit: input.Unit, CustomUnit: input.CustomUnit, Schedule: habit.Schedule{Weekdays: input.Weekdays, Time: input.Time, WeeklyTargetExecutions: input.WeeklyTargetExecutions, Reminder: input.Reminder}}
	return value, profile.Demographics{Age: input.Age, WeightHundredths: weight, HeightHundredths: height, Gender: input.Gender}, nil
}

func (h *handler) createHabit(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	var request habitRequest
	if decodeJSON(w, r, &request) != nil {
		http.Error(w, "Dados do hábito inválidos.", http.StatusBadRequest)
		return
	}
	input, demographics, err := request.domain()
	if err == nil {
		err = habit.Validate(habit.Normalize(input))
	}
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	if _, err = h.profiles.UpdateDemographics(r.Context(), identity, demographics); err != nil {
		h.writeHabitError(w, err)
		return
	}
	value, err := h.habits.Create(r.Context(), identity, userProfile.Timezone, input)
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	if err := h.reminders.Reconcile(r.Context(), identity, true); err != nil {
		http.Error(w, "Hábito salvo, mas não foi possível atualizar os lembretes.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h *handler) suggestHabit(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var request habitsuggestion.Request
	if decodeJSON(w, r, &request) != nil {
		http.Error(w, "Dados para sugestão inválidos.", http.StatusBadRequest)
		return
	}
	value, err := h.suggestions.Suggest(r.Context(), identity, request)
	if errors.Is(err, habitsuggestion.ErrInvalidRequest) {
		http.Error(w, "Preencha um título e uma descrição válidos antes de pedir uma sugestão.", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		h.logger.Warn("falha ao gerar sugestão de hábito")
		http.Error(w, "Não foi possível gerar uma sugestão agora. Tente novamente mais tarde.", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) updateHabit(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	var request habitRequest
	if decodeJSON(w, r, &request) != nil {
		http.Error(w, "Dados do hábito inválidos.", http.StatusBadRequest)
		return
	}
	input, demographics, err := request.domain()
	if err == nil {
		err = habit.Validate(habit.Normalize(input))
	}
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	if _, err = h.habits.Get(r.Context(), identity, r.PathValue("id")); err != nil {
		h.writeHabitError(w, err)
		return
	}
	currentHabit, _ := h.habits.Get(r.Context(), identity, r.PathValue("id"))
	if err = h.syncHabit(r, identity, userProfile, currentHabit); err != nil {
		h.writeHabitError(w, err)
		return
	}
	if _, err = h.profiles.UpdateDemographics(r.Context(), identity, demographics); err != nil {
		h.writeHabitError(w, err)
		return
	}
	value, err := h.habits.Update(r.Context(), identity, userProfile.Timezone, r.PathValue("id"), input)
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	if err := h.reminders.Reconcile(r.Context(), identity, true); err != nil {
		http.Error(w, "Hábito salvo, mas não foi possível atualizar os lembretes.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) duplicateHabit(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err == nil {
		var value habit.Habit
		value, err = h.habits.Duplicate(r.Context(), identity, userProfile.Timezone, r.PathValue("id"))
		if err == nil {
			if err = h.reminders.Reconcile(r.Context(), identity, true); err != nil {
				h.writeHabitError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, value)
			return
		}
	}
	h.writeHabitError(w, err)
}
func (h *handler) archiveHabit(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	current, err := h.habits.Get(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	if err = h.syncHabit(r, identity, userProfile, current); err != nil {
		h.writeHabitError(w, err)
		return
	}
	value, err := h.habits.Archive(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	if err := h.reminders.Reconcile(r.Context(), identity, true); err != nil {
		h.writeHabitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) reactivateHabit(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err == nil {
		var value habit.Habit
		value, err = h.habits.Reactivate(r.Context(), identity, userProfile.Timezone, r.PathValue("id"))
		if err == nil {
			if err = h.reminders.Reconcile(r.Context(), identity, true); err != nil {
				h.writeHabitError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, value)
			return
		}
	}
	h.writeHabitError(w, err)
}
func (h *handler) deleteHabit(w http.ResponseWriter, r *http.Request) {
	identity, userProfile, err := h.authenticatedProfile(r)
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	current, err := h.habits.Get(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		h.writeHabitError(w, err)
		return
	}
	if err = h.syncHabit(r, identity, userProfile, current); err != nil {
		h.writeHabitError(w, err)
		return
	}
	if err := h.habits.Delete(r.Context(), identity, r.PathValue("id")); err != nil {
		h.writeHabitError(w, err)
		return
	}
	if err := h.reminders.Reconcile(r.Context(), identity, true); err != nil {
		h.writeHabitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *handler) listPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	values, err := h.reminders.Subscriptions(r.Context(), identity)
	if err != nil {
		http.Error(w, "Não foi possível carregar os dispositivos.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vapidPublicKey": h.vapidPublicKey, "subscriptions": values})
}

func (h *handler) registerPushSubscription(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256DH string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "Subscription inválida.", http.StatusBadRequest)
		return
	}
	value, err := h.reminders.RegisterSubscription(r.Context(), identity, input.Endpoint, input.Keys.P256DH, input.Keys.Auth)
	if errors.Is(err, reminder.ErrInvalidSubscription) {
		http.Error(w, "Subscription inválida.", http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, reminder.ErrSubscriptionLimit) {
		http.Error(w, "Limite de 10 dispositivos ativos atingido.", http.StatusConflict)
		return
	}
	if err != nil {
		h.logger.Error("falha ao registrar dispositivo de notificação")
		http.Error(w, "Não foi possível ativar as notificações.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h *handler) disablePushSubscription(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	if err := h.reminders.DisableSubscription(r.Context(), identity, r.PathValue("id")); err != nil {
		http.Error(w, "Não foi possível desativar o dispositivo.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

func (h *handler) processReminders(w http.ResponseWriter, r *http.Request) {
	// Em produção, esta rota existe somente na instância privada do mesmo binário,
	// protegida pelo IAM do Cloud Run. Ela deliberadamente não aceita sessão de usuário.
	processed, err := h.reminders.Process(r.Context())
	if err != nil {
		h.logger.Error("falha no processamento de lembretes")
		http.Error(w, "Falha no processamento.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"processed": processed})
}

func (h *handler) recordSimple(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Completed bool `json:"completed"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "Resultado inválido.", http.StatusBadRequest)
		return
	}
	value, err := h.executions.RecordSimple(r.Context(), identity, r.PathValue("id"), input.Completed)
	if err != nil {
		h.writeExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) recordQuantitative(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Achieved string `json:"achieved"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "Resultado inválido.", http.StatusBadRequest)
		return
	}
	achieved, err := parseOptionalHundredths(input.Achieved)
	if err != nil {
		h.writeExecutionError(w, execution.ErrInvalidValue)
		return
	}
	value, err := h.executions.RecordQuantitative(r.Context(), identity, r.PathValue("id"), achieved)
	if err != nil {
		h.writeExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) createNote(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		ExecutionID string `json:"executionId"`
		Content     string `json:"content"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "Nota inválida.", http.StatusBadRequest)
		return
	}
	value, err := h.notes.Create(r.Context(), identity, r.PathValue("id"), input.ExecutionID, input.Content)
	if err != nil {
		h.writeExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *handler) updateNote(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	var input struct {
		Content string `json:"content"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "Nota inválida.", http.StatusBadRequest)
		return
	}
	value, err := h.notes.Update(r.Context(), identity, r.PathValue("id"), input.Content)
	if err != nil {
		h.writeExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) deleteNote(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.IdentityFromContext(r.Context())
	if err := h.notes.Delete(r.Context(), identity, r.PathValue("id")); err != nil {
		h.writeExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (h *handler) writeExecutionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, execution.ErrNotFound), errors.Is(err, note.ErrNotFound), errors.Is(err, habit.ErrNotFound):
		http.Error(w, "Registro não encontrado.", http.StatusNotFound)
	case errors.Is(err, execution.ErrClosed):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, execution.ErrInvalidValue), errors.Is(err, execution.ErrNotScheduled), errors.Is(err, note.ErrInvalidContent):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		h.logger.Error("falha em execução ou nota", "error", err)
		http.Error(w, "Não foi possível concluir a operação.", http.StatusInternalServerError)
	}
}

func (h *handler) writeHabitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, habit.ErrNotFound):
		http.Error(w, "Hábito não encontrado.", http.StatusNotFound)
	case errors.Is(err, habit.ErrInvalidInput), errors.Is(err, habit.ErrInvalidGoal), errors.Is(err, habit.ErrInvalidSchedule), errors.Is(err, habit.ErrInvalidWeekly), errors.Is(err, habit.ErrInvalidUnit), errors.Is(err, habit.ErrInvalidReminder), errors.Is(err, habit.ErrInvalidTransition), errors.Is(err, profile.ErrInvalidAge), errors.Is(err, profile.ErrInvalidWeight), errors.Is(err, profile.ErrInvalidHeight), errors.Is(err, profile.ErrInvalidGender):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		h.logger.Error("falha na operação de hábito", "error", err)
		http.Error(w, "Não foi possível concluir a operação.", http.StatusInternalServerError)
	}
}

func parseOptionalHundredths(value string) (int64, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	if value == "" {
		return 0, nil
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, habit.ErrInvalidGoal
	}
	if len(parts) == 2 && len(parts[1]) > 2 {
		return 0, habit.ErrInvalidGoal
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, habit.ErrInvalidGoal
	}
	fraction := int64(0)
	if len(parts) == 2 {
		fractionText := parts[1]
		if len(fractionText) == 1 {
			fractionText += "0"
		}
		if fractionText != "" {
			fraction, err = strconv.ParseInt(fractionText, 10, 64)
			if err != nil {
				return 0, habit.ErrInvalidGoal
			}
		}
	}
	return whole*100 + fraction, nil
}

func parsePositiveOptionalHundredths(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := parseOptionalHundredths(trimmed)
	if err != nil || (trimmed != "" && parsed <= 0) {
		return 0, habit.ErrInvalidGoal
	}
	return parsed, nil
}

func formatHundredths(value int64) string {
	if value == 0 {
		return ""
	}
	if value%100 == 0 {
		return strconv.FormatInt(value/100, 10)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", float64(value)/100), "0"), ".")
}
func unitLabel(value habit.Unit) string {
	return map[habit.Unit]string{habit.UnitPages: "páginas", habit.UnitMinutes: "minutos", habit.UnitKilometers: "km", habit.UnitTimes: "vezes", habit.UnitLiters: "litros", habit.UnitOther: "Outra"}[value]
}
func weekdayLabel(value int) string {
	return map[int]string{1: "Seg", 2: "Ter", 3: "Qua", 4: "Qui", 5: "Sex", 6: "Sáb", 7: "Dom"}[value]
}
func statusLabel(value habit.Status) string {
	if value == habit.StatusArchived {
		return "Arquivado"
	}
	return "Ativo"
}
func reminderLabel(value habit.ReminderChannel) string {
	return map[habit.ReminderChannel]string{habit.ReminderNotification: "Notificação", habit.ReminderEmail: "E-mail", habit.ReminderBoth: "Ambos"}[value]
}
func executionStatusLabel(value execution.Status) string {
	return map[execution.Status]string{execution.StatusPending: "Pendente", execution.StatusCompleted: "Concluído", execution.StatusPartial: "Parcial", execution.StatusNotDone: "Não realizado"}[value]
}
func hasWeekday(days []int, want int) bool {
	for _, day := range days {
		if day == want {
			return true
		}
	}
	return false
}

func ratePercent(value progress.Rate) int {
	if value.Denominator == 0 || value.Contribution == nil {
		return 0
	}
	percentage := new(big.Rat).Mul(new(big.Rat).Set(value.Contribution), big.NewRat(100, int64(value.Denominator)))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(percentage.Num(), percentage.Denom(), remainder)
	if new(big.Int).Mul(remainder, big.NewInt(2)).Cmp(percentage.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return int(quotient.Int64())
}

func countPercent(count, total int) int {
	if total == 0 {
		return 0
	}
	return int((int64(count)*100 + int64(total)/2) / int64(total))
}

func shortDate(value string) string {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return parsed.Format("02/01")
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

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("conteúdo JSON adicional")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler, allowLocalEmulator, production bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectSources := "'self' https://identitytoolkit.googleapis.com https://securetoken.googleapis.com"
		if allowLocalEmulator {
			connectSources += " http://127.0.0.1:* http://localhost:*"
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self' https://www.gstatic.com; connect-src "+connectSources+"; worker-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(), payment=(), usb=()")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if production {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if privateResponse(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func privateResponse(path string) bool {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/media/") || strings.HasPrefix(path, "/internal/") {
		return true
	}
	switch path {
	case "/perfil", "/alterar-senha", "/exclusao-conta", "/criar-habito", "/meus-habitos", "/progresso", "/recompensas":
		return true
	}
	return strings.HasPrefix(path, "/habitos/")
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(value)
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		wrapped := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("requisição HTTP", "request_id", requestIDFromContext(r.Context()), "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds())
	})
}

type requestIDKey struct{}

func requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			raw = [16]byte{}
		}
		requestID := hex.EncodeToString(raw[:])
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
	})
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

type limiterWindow struct {
	expires time.Time
	count   int
}

type limiterRegistry struct {
	mu      sync.Mutex
	windows map[string]limiterWindow
	checks  uint64
}

func newLimiterRegistry() *limiterRegistry {
	return &limiterRegistry{windows: make(map[string]limiterWindow)}
}

func (l *limiterRegistry) allow(key string, limit int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checks++
	if l.checks%256 == 0 {
		for existingKey, existing := range l.windows {
			if !now.Before(existing.expires) {
				delete(l.windows, existingKey)
			}
		}
	}
	value := l.windows[key]
	if value.expires.IsZero() || !now.Before(value.expires) {
		value = limiterWindow{expires: now.Add(window)}
	}
	if value.count >= limit {
		return false
	}
	value.count++
	l.windows[key] = value
	return true
}

func (h *handler) rateLimit(scope string, limit int, window time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		if identity, ok := auth.IdentityFromContext(r.Context()); ok && identity.UID != "" {
			key = identity.UID
		}
		if !h.limiters.allow(scope+":"+key, limit, window, time.Now()) {
			w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
			http.Error(w, "Muitas tentativas. Aguarde e tente novamente.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientKey(r *http.Request) string {
	if cookie, err := r.Cookie(csrf.CookieName); err == nil && cookie.Value != "" {
		sum := sha256.Sum256([]byte(cookie.Value))
		return "csrf-" + hex.EncodeToString(sum[:8])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"habitos/internal/auth"
	"habitos/internal/config"
	"habitos/internal/execution"
	"habitos/internal/firebaseadmin"
	"habitos/internal/habit"
	"habitos/internal/habitsuggestion"
	"habitos/internal/httpserver"
	"habitos/internal/note"
	"habitos/internal/profile"
	"habitos/internal/progress"
	"habitos/internal/ranking"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	appConfig, err := config.Load()
	if err != nil {
		logger.Error("configuração inválida", "error", err)
		os.Exit(1)
	}

	clients, err := firebaseadmin.New(context.Background(), appConfig.FirebaseProjectID)
	if err != nil {
		logger.Error("falha ao inicializar serviços Firebase", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := clients.Close(); err != nil {
			logger.Error("falha ao fechar Firestore", "error", err)
		}
	}()

	sessions := auth.NewFirebaseSessionManager(clients.Auth)
	profiles := profile.NewService(profile.NewFirestoreRepository(clients.Firestore))
	habits := habit.NewService(habit.NewFirestoreRepository(clients.Firestore))
	executions := execution.NewService(execution.NewFirestoreRepository(clients.Firestore))
	notes := note.NewService(note.NewFirestoreRepository(clients.Firestore), habits, executions)
	progressService := progress.NewService(progress.NewFirestoreRepository(clients.Firestore))
	rankingService := ranking.NewService(ranking.NewFirestoreRepository(clients.Firestore))
	suggestionService := habitsuggestion.NewService(habitsuggestion.NewOpenAIProvider(habitsuggestion.OpenAIConfig{APIKey: appConfig.OpenAIAPIKey, Model: appConfig.OpenAIModel}), appConfig.AIRequestTimeout)
	server, err := httpserver.New(httpserver.Config{
		Port:          appConfig.Port,
		Logger:        logger,
		SecureCookies: appConfig.SecureCookies,
		FirebaseWeb: httpserver.FirebaseWebConfig{
			APIKey:          appConfig.FirebaseWebAPIKey,
			AuthDomain:      appConfig.FirebaseAuthDomain,
			ProjectID:       appConfig.FirebaseProjectID,
			AppID:           appConfig.FirebaseAppID,
			AuthEmulatorURL: appConfig.AuthEmulatorURL,
		},
	}, httpserver.Dependencies{
		Sessions:    sessions,
		Profiles:    profiles,
		Habits:      habits,
		Executions:  executions,
		Notes:       notes,
		Progress:    progressService,
		Ranking:     rankingService,
		Suggestions: suggestionService,
	})
	if err != nil {
		logger.Error("falha ao configurar o servidor", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("servidor iniciado", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("servidor encerrado inesperadamente", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("falha no encerramento do servidor", "error", err)
			os.Exit(1)
		}
		logger.Info("servidor encerrado")
	}
}

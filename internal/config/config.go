package config

import (
	"fmt"
	"os"
)

const (
	LocalEmulatorProjectID     = "demo-habitos-local"
	LocalAuthEmulatorHost      = "127.0.0.1:9099"
	LocalFirestoreEmulatorHost = "127.0.0.1:8081"
)

type Config struct {
	Port               string
	Environment        string
	SecureCookies      bool
	FirebaseProjectID  string
	FirebaseWebAPIKey  string
	FirebaseAuthDomain string
	FirebaseAppID      string
	AuthEmulatorURL    string
}

func Load() (Config, error) {
	environment := envOrDefault("APP_ENV", "development")
	config := Config{
		Port:               envOrDefault("PORT", "8080"),
		Environment:        environment,
		SecureCookies:      environment == "production",
		FirebaseProjectID:  os.Getenv("FIREBASE_PROJECT_ID"),
		FirebaseWebAPIKey:  os.Getenv("FIREBASE_WEB_API_KEY"),
		FirebaseAuthDomain: os.Getenv("FIREBASE_AUTH_DOMAIN"),
		FirebaseAppID:      os.Getenv("FIREBASE_APP_ID"),
	}

	if value := os.Getenv("SESSION_COOKIE_SECURE"); value != "" {
		switch value {
		case "true":
			config.SecureCookies = true
		case "false":
			config.SecureCookies = false
		default:
			return Config{}, fmt.Errorf("SESSION_COOKIE_SECURE deve ser true ou false")
		}
	}

	authEmulatorHost := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	firestoreEmulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if authEmulatorHost != "" {
		config.AuthEmulatorURL = "http://" + authEmulatorHost
	}

	missing := make([]string, 0, 4)
	for name, value := range map[string]string{
		"FIREBASE_PROJECT_ID":  config.FirebaseProjectID,
		"FIREBASE_WEB_API_KEY": config.FirebaseWebAPIKey,
		"FIREBASE_AUTH_DOMAIN": config.FirebaseAuthDomain,
		"FIREBASE_APP_ID":      config.FirebaseAppID,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("variáveis obrigatórias ausentes: %v", missing)
	}
	if environment == "production" && !config.SecureCookies {
		return Config{}, fmt.Errorf("SESSION_COOKIE_SECURE não pode ser false em produção")
	}
	usingEmulators := authEmulatorHost != "" || firestoreEmulatorHost != ""
	if usingEmulators {
		if authEmulatorHost != LocalAuthEmulatorHost || firestoreEmulatorHost != LocalFirestoreEmulatorHost {
			return Config{}, fmt.Errorf("emuladores locais exigem FIREBASE_AUTH_EMULATOR_HOST=%s e FIRESTORE_EMULATOR_HOST=%s", LocalAuthEmulatorHost, LocalFirestoreEmulatorHost)
		}
		if config.FirebaseProjectID != LocalEmulatorProjectID {
			return Config{}, fmt.Errorf("emuladores exigem FIREBASE_PROJECT_ID=%s", LocalEmulatorProjectID)
		}
		if googleProjectID := os.Getenv("GCLOUD_PROJECT"); googleProjectID != "" && googleProjectID != LocalEmulatorProjectID {
			return Config{}, fmt.Errorf("GCLOUD_PROJECT deve ser %s ao usar emuladores", LocalEmulatorProjectID)
		}
	}

	return config, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

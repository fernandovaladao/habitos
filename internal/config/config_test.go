package config

import (
	"testing"
	"time"
)

func resetConfigEnvironment(t *testing.T) {
	t.Helper()
	variables := []string{
		"PORT",
		"APP_ENV",
		"SESSION_COOKIE_SECURE",
		"FIREBASE_PROJECT_ID",
		"FIREBASE_WEB_API_KEY",
		"FIREBASE_AUTH_DOMAIN",
		"FIREBASE_APP_ID",
		"FIREBASE_AUTH_EMULATOR_HOST",
		"FIRESTORE_EMULATOR_HOST",
		"GCLOUD_PROJECT",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"FIREBASE_CONFIG",
		"OPENAI_API_KEY",
		"OPENAI_MODEL",
		"AI_REQUEST_TIMEOUT",
	}
	for _, name := range variables {
		t.Setenv(name, "")
	}
}

func setRequiredFirebaseEnvironment(t *testing.T) {
	t.Helper()
	resetConfigEnvironment(t)
	t.Setenv("FIREBASE_PROJECT_ID", "project")
	t.Setenv("FIREBASE_WEB_API_KEY", "public-key")
	t.Setenv("FIREBASE_AUTH_DOMAIN", "project.firebaseapp.com")
	t.Setenv("FIREBASE_APP_ID", "app-id")
	t.Setenv("OPENAI_API_KEY", "openai-test-key")
	t.Setenv("OPENAI_MODEL", "gpt-5.6-luna")
	t.Setenv("AI_REQUEST_TIMEOUT", "10s")
}

func TestLoadReadsOpenAIConfiguration(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("OPENAI_MODEL", "modelo-configuravel")
	t.Setenv("AI_REQUEST_TIMEOUT", "7s")

	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.OpenAIAPIKey != "openai-test-key" || config.OpenAIModel != "modelo-configuravel" || config.AIRequestTimeout != 7*time.Second {
		t.Fatalf("configuração OpenAI = %#v", config)
	}
}

func TestLoadRejectsInvalidAITimeout(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("AI_REQUEST_TIMEOUT", "zero")
	if _, err := Load(); err == nil {
		t.Fatal("timeout inválido deveria ser rejeitado")
	}
}

func TestLoadUsesInsecureCookieOnlyInDevelopment(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SESSION_COOKIE_SECURE", "false")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() erro = %v", err)
	}
	if config.SecureCookies {
		t.Fatal("cookie deveria aceitar HTTP no desenvolvimento")
	}
}

func TestLoadRejectsInsecureCookieInProduction(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "false")

	if _, err := Load(); err == nil {
		t.Fatal("Load() deveria rejeitar cookie inseguro em produção")
	}
}

func TestLoadAcceptsValidLocalEmulatorConfiguration(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("FIREBASE_PROJECT_ID", LocalEmulatorProjectID)
	t.Setenv("GCLOUD_PROJECT", LocalEmulatorProjectID)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", LocalFirestoreEmulatorHost)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() erro = %v", err)
	}
	if config.FirebaseProjectID != LocalEmulatorProjectID {
		t.Fatalf("project ID = %q", config.FirebaseProjectID)
	}
	if config.AuthEmulatorURL != "http://127.0.0.1:9099" {
		t.Fatalf("AuthEmulatorURL = %q", config.AuthEmulatorURL)
	}
}

func TestLoadRejectsRealProjectWithEmulators(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("FIREBASE_PROJECT_ID", "projeto-real")
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", LocalFirestoreEmulatorHost)

	if _, err := Load(); err == nil {
		t.Fatal("Load() deveria rejeitar projeto diferente do demo local")
	}
}

func TestLoadRejectsPartialEmulatorConfiguration(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("FIREBASE_PROJECT_ID", LocalEmulatorProjectID)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() deveria exigir Auth e Firestore Emulator juntos")
	}
}

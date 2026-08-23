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
		"FIREBASE_STORAGE_BUCKET",
		"FIREBASE_AUTH_EMULATOR_HOST",
		"FIRESTORE_EMULATOR_HOST",
		"FIREBASE_STORAGE_EMULATOR_HOST",
		"GCLOUD_PROJECT",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"FIREBASE_CONFIG",
		"OPENAI_API_KEY",
		"OPENAI_MODEL",
		"AI_REQUEST_TIMEOUT",
		"APP_BASE_URL", "VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "VAPID_SUBSCRIBER",
		"RESEND_API_KEY", "EMAIL_FROM", "EMAIL_REQUEST_TIMEOUT", "REMINDER_PROCESSOR_ENABLED", "HTTP_WRITE_TIMEOUT",
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
	t.Setenv("FIREBASE_STORAGE_BUCKET", "project.appspot.com")
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

func TestLoadRequiresOpenAIKeyForWebRole(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("OPENAI_API_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("serviço web deveria exigir OPENAI_API_KEY")
	}
}

func TestProductionWebAcceptsOpenAIWithoutReminderSecrets(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_BASE_URL", "https://habitos.example.test")
	t.Setenv("VAPID_PUBLIC_KEY", "public")

	value, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if value.ReminderProcessor || value.OpenAIAPIKey != "openai-test-key" || value.ResendAPIKey != "" || value.VAPIDPrivateKey != "" {
		t.Fatalf("configuração=%#v", value)
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

func TestLoadRejectsEmulatorsInProduction(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("APP_BASE_URL", "https://habitos.example.test")
	t.Setenv("VAPID_PUBLIC_KEY", "public")
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", LocalFirestoreEmulatorHost)
	t.Setenv("FIREBASE_STORAGE_EMULATOR_HOST", LocalStorageEmulatorHost)
	if _, err := Load(); err == nil {
		t.Fatal("produção não deveria aceitar hosts de Emulator")
	}
}

func TestLoadRequiresHTTPSBaseURLInProduction(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("APP_BASE_URL", "http://habitos.example.test")
	t.Setenv("VAPID_PUBLIC_KEY", "public")
	if _, err := Load(); err == nil {
		t.Fatal("produção não deveria aceitar APP_BASE_URL sem HTTPS")
	}
}

func TestLoadUsesRoleSpecificWriteTimeout(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	web, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if web.HTTPWriteTimeout != 30*time.Second {
		t.Fatalf("timeout web = %v", web.HTTPWriteTimeout)
	}

	t.Setenv("FIREBASE_PROJECT_ID", LocalEmulatorProjectID)
	t.Setenv("GCLOUD_PROJECT", LocalEmulatorProjectID)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", LocalFirestoreEmulatorHost)
	t.Setenv("FIREBASE_STORAGE_EMULATOR_HOST", LocalStorageEmulatorHost)
	t.Setenv("REMINDER_PROCESSOR_ENABLED", "true")
	processor, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if processor.HTTPWriteTimeout != 10*time.Minute {
		t.Fatalf("timeout processador = %v", processor.HTTPWriteTimeout)
	}
}

func TestLoadAcceptsValidLocalEmulatorConfiguration(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("FIREBASE_PROJECT_ID", LocalEmulatorProjectID)
	t.Setenv("GCLOUD_PROJECT", LocalEmulatorProjectID)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", LocalFirestoreEmulatorHost)
	t.Setenv("FIREBASE_STORAGE_EMULATOR_HOST", LocalStorageEmulatorHost)

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

func TestReminderProcessorLocalBypassRequiresDemoEmulators(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("REMINDER_PROCESSOR_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("processador local deveria exigir projeto demo e emuladores")
	}
	t.Setenv("FIREBASE_PROJECT_ID", LocalEmulatorProjectID)
	t.Setenv("GCLOUD_PROJECT", LocalEmulatorProjectID)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", LocalFirestoreEmulatorHost)
	t.Setenv("FIREBASE_STORAGE_EMULATOR_HOST", LocalStorageEmulatorHost)
	value, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !value.ReminderProcessor {
		t.Fatal("processador local válido não foi habilitado")
	}
}

func TestProductionProcessorRequiresBackendReminderSecrets(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("APP_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("VAPID_PUBLIC_KEY", "public")
	t.Setenv("REMINDER_PROCESSOR_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("processador de produção aceitou segredos ausentes")
	}
	for name, value := range map[string]string{"VAPID_PRIVATE_KEY": "private", "VAPID_SUBSCRIBER": "mailto:test@example.test", "RESEND_API_KEY": "resend-secret", "EMAIL_FROM": "HÁBITOS <test@example.test>"} {
		t.Setenv(name, value)
	}
	t.Setenv("APP_BASE_URL", "https://habitos.example.test")
	value, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !value.ReminderProcessor || value.VAPIDPrivateKey != "private" || value.OpenAIAPIKey != "" {
		t.Fatalf("configuração=%#v", value)
	}
}

func TestProductionProcessorRejectsOpenAIKey(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_BASE_URL", "https://habitos.example.test")
	t.Setenv("VAPID_PUBLIC_KEY", "public")
	t.Setenv("VAPID_PRIVATE_KEY", "private")
	t.Setenv("VAPID_SUBSCRIBER", "mailto:test@example.test")
	t.Setenv("RESEND_API_KEY", "resend-secret")
	t.Setenv("EMAIL_FROM", "HÁBITOS <test@example.test>")
	t.Setenv("REMINDER_PROCESSOR_ENABLED", "true")

	if _, err := Load(); err == nil {
		t.Fatal("processador de produção não deveria aceitar OPENAI_API_KEY")
	}
}

func TestLoadRejectsRealProjectWithEmulators(t *testing.T) {
	setRequiredFirebaseEnvironment(t)
	t.Setenv("FIREBASE_PROJECT_ID", "projeto-real")
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", LocalAuthEmulatorHost)
	t.Setenv("FIRESTORE_EMULATOR_HOST", LocalFirestoreEmulatorHost)
	t.Setenv("FIREBASE_STORAGE_EMULATOR_HOST", LocalStorageEmulatorHost)

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
		t.Fatal("Load() deveria exigir Auth, Firestore e Storage Emulator juntos")
	}
}

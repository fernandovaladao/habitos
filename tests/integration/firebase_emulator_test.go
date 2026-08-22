package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"habitos/internal/auth"
	"habitos/internal/config"
	"habitos/internal/firebaseadmin"
	"habitos/internal/profile"
)

func TestAuthenticationAndProfileWithFirebaseEmulators(t *testing.T) {
	if os.Getenv("RUN_FIREBASE_EMULATOR_TESTS") != "1" {
		t.Skip("teste opt-in: defina RUN_FIREBASE_EMULATOR_TESTS=1 com os emuladores ativos")
	}
	requireLocalEnvironment(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	clients, err := firebaseadmin.New(ctx, config.LocalEmulatorProjectID)
	if err != nil {
		t.Fatalf("inicializar clientes Firebase: %v", err)
	}
	t.Cleanup(func() { _ = clients.Close() })

	email := fmt.Sprintf("e2e-%d@example.test", time.Now().UnixNano())
	idToken, uid := createEmulatorUser(t, ctx, email)
	t.Cleanup(func() { deleteEmulatorUser(t, idToken) })
	t.Cleanup(func() {
		_, _ = clients.Firestore.Collection("users").Doc(uid).Delete(context.Background())
	})

	sessions := auth.NewFirebaseSessionManager(clients.Auth)
	sessionCookie, err := sessions.CreateSession(ctx, idToken, auth.SessionDuration)
	if err != nil {
		t.Fatalf("criar sessão: %v", err)
	}
	identity, err := sessions.VerifySession(ctx, sessionCookie)
	if err != nil {
		t.Fatalf("validar sessão: %v", err)
	}
	if identity.UID != uid || identity.Email != email {
		t.Fatalf("identidade = %#v, esperado UID %q e e-mail %q", identity, uid, email)
	}

	profiles := profile.NewService(profile.NewFirestoreRepository(clients.Firestore))
	first, err := profiles.EnsureProfile(ctx, identity, "America/Sao_Paulo")
	if err != nil {
		t.Fatalf("criar perfil mínimo: %v", err)
	}
	second, err := profiles.EnsureProfile(ctx, identity, "UTC")
	if err != nil {
		t.Fatalf("repetir criação do perfil: %v", err)
	}
	if first != second {
		t.Fatalf("EnsureProfile não foi idempotente: primeiro=%#v segundo=%#v", first, second)
	}
	if first.RankingOptIn || first.ProfileComplete {
		t.Fatalf("perfil mínimo deveria ser privado e incompleto: %#v", first)
	}
}

func requireLocalEnvironment(t *testing.T) {
	t.Helper()
	want := map[string]string{
		"FIREBASE_PROJECT_ID":         config.LocalEmulatorProjectID,
		"GCLOUD_PROJECT":              config.LocalEmulatorProjectID,
		"FIREBASE_AUTH_EMULATOR_HOST": config.LocalAuthEmulatorHost,
		"FIRESTORE_EMULATOR_HOST":     config.LocalFirestoreEmulatorHost,
	}
	for name, expected := range want {
		if value := os.Getenv(name); value != expected {
			t.Fatalf("%s=%q; esperado %q para impedir acesso a recursos reais", name, value, expected)
		}
	}
}

func createEmulatorUser(t *testing.T, ctx context.Context, email string) (string, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"email":             email,
		"password":          "senha-local-123",
		"returnSecureToken": true,
	})
	if err != nil {
		t.Fatalf("codificar cadastro: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:9099/identitytoolkit.googleapis.com/v1/accounts:signUp?key=demo-key", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("criar requisição de cadastro: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("cadastrar no Auth Emulator: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Auth Emulator retornou status %d", response.StatusCode)
	}
	var result struct {
		IDToken string `json:"idToken"`
		LocalID string `json:"localId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decodificar cadastro: %v", err)
	}
	if result.IDToken == "" || result.LocalID == "" {
		t.Fatalf("cadastro sem token ou UID: %#v", result)
	}
	return result.IDToken, result.LocalID
}

func deleteEmulatorUser(t *testing.T, idToken string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"idToken": idToken})
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:9099/identitytoolkit.googleapis.com/v1/accounts:delete?key=demo-key", bytes.NewReader(body))
	if err != nil {
		t.Logf("não foi possível preparar limpeza do Auth Emulator: %v", err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Logf("não foi possível limpar usuário do Auth Emulator: %v", err)
		return
	}
	_ = response.Body.Close()
}

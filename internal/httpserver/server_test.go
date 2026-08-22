package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"habitos/internal/auth"
	"habitos/internal/csrf"
	"habitos/internal/profile"
)

type fakeSessionManager struct {
	identity        auth.Identity
	verifyErr       error
	createdDuration time.Duration
	createdIDToken  string
}

func (f *fakeSessionManager) CreateSession(_ context.Context, idToken string, duration time.Duration) (string, error) {
	f.createdIDToken = idToken
	f.createdDuration = duration
	return "firebase-session-cookie", nil
}

func (f *fakeSessionManager) VerifySession(_ context.Context, _ string) (auth.Identity, error) {
	return f.identity, f.verifyErr
}

type fakeProfileRepository struct {
	profiles    map[string]profile.Profile
	ensureCalls int
	lastUID     string
}

func newFakeProfileRepository() *fakeProfileRepository {
	return &fakeProfileRepository{profiles: make(map[string]profile.Profile)}
}

func (r *fakeProfileRepository) Ensure(_ context.Context, candidate profile.Profile) (profile.Profile, error) {
	r.ensureCalls++
	if existing, ok := r.profiles[candidate.UID]; ok {
		return existing, nil
	}
	r.profiles[candidate.UID] = candidate
	return candidate, nil
}

func (r *fakeProfileRepository) Get(_ context.Context, uid string) (profile.Profile, error) {
	value, ok := r.profiles[uid]
	if !ok {
		return profile.Profile{}, profile.ErrNotFound
	}
	return value, nil
}

func (r *fakeProfileRepository) Update(_ context.Context, uid string, update profile.Update, updatedAt time.Time) (profile.Profile, error) {
	r.lastUID = uid
	value, ok := r.profiles[uid]
	if !ok {
		return profile.Profile{}, profile.ErrNotFound
	}
	value.Nickname = update.Nickname
	value.Age = update.Age
	value.Timezone = update.Timezone
	value.RankingOptIn = update.RankingOptIn
	value.ProfileComplete = true
	value.UpdatedAt = updatedAt
	r.profiles[uid] = value
	return value, nil
}

type testApp struct {
	handler  http.Handler
	sessions *fakeSessionManager
	profiles *fakeProfileRepository
}

func newTestApp(t *testing.T) testApp {
	t.Helper()
	sessions := &fakeSessionManager{identity: auth.Identity{UID: "firebase-user-a", Email: "a@example.com"}}
	profiles := newFakeProfileRepository()
	handler, err := NewHandler(Config{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		FirebaseWeb: FirebaseWebConfig{APIKey: "public-key", ProjectID: "test-project"},
	}, Dependencies{Sessions: sessions, Profiles: profile.NewService(profiles)})
	if err != nil {
		t.Fatalf("NewHandler() retornou erro: %v", err)
	}
	return testApp{handler: handler, sessions: sessions, profiles: profiles}
}

func TestHealth(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	app.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil || body["status"] != "ok" {
		t.Fatalf("resposta inesperada: %#v, erro=%v", body, err)
	}
}

func TestPublicRoutes(t *testing.T) {
	app := newTestApp(t)
	routes := []string{"/", "/entrar", "/cadastro", "/recuperar-senha", "/aprenda-4rs"}
	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
			}
		})
	}
}

func TestPrivateRouteRedirectsAnonymousUser(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/perfil", nil))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/entrar" {
		t.Fatalf("status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestPrivateRouteUsesValidatedSession(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/perfil?userId=firebase-user-b", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
	}
	if _, ok := app.profiles.profiles["firebase-user-a"]; !ok {
		t.Fatal("perfil do UID autenticado não foi usado")
	}
	if _, ok := app.profiles.profiles["firebase-user-b"]; ok {
		t.Fatal("userId enviado pelo cliente foi usado")
	}
}

func TestSessionCreationRequiresCSRFAndDoesNotCreateProfile(t *testing.T) {
	app := newTestApp(t)

	withoutCSRF := httptest.NewRecorder()
	app.handler.ServeHTTP(withoutCSRF, httptest.NewRequest(http.MethodPost, "/api/auth/session", strings.NewReader(`{"idToken":"token"}`)))
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("sem CSRF: status = %d", withoutCSRF.Code)
	}

	token, cookie := issueCSRF(t, app.handler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/session", strings.NewReader(`{"idToken":"firebase-id-token"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(cookie)
	app.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, corpo=%s", recorder.Code, recorder.Body.String())
	}
	if app.sessions.createdDuration != auth.SessionDuration || app.sessions.createdIDToken != "firebase-id-token" {
		t.Fatalf("sessão criada com token=%q duração=%s", app.sessions.createdIDToken, app.sessions.createdDuration)
	}
	if app.profiles.ensureCalls != 0 {
		t.Fatalf("endpoint de sessão chamou EnsureProfile %d vez(es)", app.profiles.ensureCalls)
	}

	var sessionCookie *http.Cookie
	for _, value := range recorder.Result().Cookies() {
		if value.Name == auth.SessionCookieName {
			sessionCookie = value
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.Path != "/" || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("atributos do cookie de sessão inválidos: %#v", sessionCookie)
	}
}

func TestEnsureAndUpdateProfileUseAuthenticatedUID(t *testing.T) {
	app := newTestApp(t)
	token, csrfCookie := issueCSRF(t, app.handler)
	sessionCookie := &http.Cookie{Name: auth.SessionCookieName, Value: "valid"}

	ensureRecorder := httptest.NewRecorder()
	ensureRequest := httptest.NewRequest(http.MethodPost, "/api/profile/ensure?userId=firebase-user-b", strings.NewReader(`{"timezone":"America/Sao_Paulo"}`))
	ensureRequest.Header.Set(csrf.HeaderName, token)
	ensureRequest.AddCookie(csrfCookie)
	ensureRequest.AddCookie(sessionCookie)
	app.handler.ServeHTTP(ensureRecorder, ensureRequest)
	if ensureRecorder.Code != http.StatusOK {
		t.Fatalf("EnsureProfile status=%d corpo=%s", ensureRecorder.Code, ensureRecorder.Body.String())
	}

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/profile?userId=firebase-user-b", bytes.NewBufferString(`{"nickname":"Pessoa A","age":16,"timezone":"America/Sao_Paulo","rankingOptIn":true}`))
	updateRequest.Header.Set(csrf.HeaderName, token)
	updateRequest.AddCookie(csrfCookie)
	updateRequest.AddCookie(sessionCookie)
	app.handler.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("Update status=%d corpo=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	if app.profiles.lastUID != "firebase-user-a" {
		t.Fatalf("UID atualizado = %q", app.profiles.lastUID)
	}
}

func TestStaticAssetAndServiceWorker(t *testing.T) {
	app := newTestApp(t)
	for _, path := range []string{"/static/css/app.css", "/static/js/app.js", "/static/js/firebase-client.js", "/static/manifest.webmanifest", "/service-worker.js"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d", recorder.Code)
			}
		})
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nao-existe", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusNotFound)
	}
}

func issueCSRF(t *testing.T, handler http.Handler) (string, *http.Cookie) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/csrf", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("emitir CSRF: status=%d", recorder.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decodificar CSRF: %v", err)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == csrf.CookieName {
			return body["csrfToken"], cookie
		}
	}
	t.Fatal("cookie CSRF ausente")
	return "", nil
}

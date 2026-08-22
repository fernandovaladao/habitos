package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeSessions struct {
	identity Identity
	err      error
}

func (f fakeSessions) CreateSession(context.Context, string, time.Duration) (string, error) {
	return "session", nil
}

func (f fakeSessions) VerifySession(context.Context, string) (Identity, error) {
	return f.identity, f.err
}

func TestRequireAPIRejectsMissingSession(t *testing.T) {
	middleware := NewMiddleware(fakeSessions{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/private", nil)

	middleware.RequireAPI(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler privado não deveria ser chamado")
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRequireAPIRejectsInvalidSession(t *testing.T) {
	middleware := NewMiddleware(fakeSessions{err: errors.New("inválida")})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/private", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "invalid"})

	middleware.RequireAPI(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler privado não deveria ser chamado")
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRequireAPIUsesValidatedIdentity(t *testing.T) {
	want := Identity{UID: "firebase-user-a", Email: "a@example.com"}
	middleware := NewMiddleware(fakeSessions{identity: want})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/private?userId=user-b", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "valid"})

	middleware.RequireAPI(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok := IdentityFromContext(r.Context())
		if !ok || got != want {
			t.Fatalf("identidade = %#v, esperada %#v", got, want)
		}
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
	}
}

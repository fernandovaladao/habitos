package auth

import (
	"context"
	"net/http"
)

const SessionCookieName = "habitos_session"

type identityContextKey struct{}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok && identity.UID != "" && identity.Email != ""
}

type Middleware struct {
	sessions SessionManager
	accounts AccountState
}

type AccountState interface {
	IsDeleting(context.Context, string) (bool, error)
}

func NewMiddleware(sessions SessionManager) *Middleware {
	return &Middleware{sessions: sessions}
}

func NewActiveMiddleware(sessions SessionManager, accounts AccountState) *Middleware {
	return &Middleware{sessions: sessions, accounts: accounts}
}

func (m *Middleware) RequireAPI(next http.Handler) http.Handler {
	return m.require(next, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Autenticação necessária.", http.StatusUnauthorized)
	})
}

func (m *Middleware) RequirePage(next http.Handler) http.Handler {
	return m.require(next, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/entrar", http.StatusSeeOther)
	})
}

func (m *Middleware) require(next http.Handler, unauthorized http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			unauthorized(w, r)
			return
		}
		identity, err := m.sessions.VerifySession(r.Context(), cookie.Value)
		if err != nil {
			unauthorized(w, r)
			return
		}
		if m.accounts != nil {
			deleting, err := m.accounts.IsDeleting(r.Context(), identity.UID)
			if err != nil || deleting {
				if deleting && r.Method == http.MethodGet {
					http.Redirect(w, r, "/exclusao-conta", http.StatusSeeOther)
					return
				}
				http.Error(w, "A conta está em processo de exclusão.", http.StatusConflict)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
	})
}

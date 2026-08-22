package auth

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidSession = errors.New("sessão inválida")

const SessionDuration = 5 * 24 * time.Hour

type Identity struct {
	UID   string
	Email string
}

type SessionManager interface {
	CreateSession(ctx context.Context, idToken string, duration time.Duration) (string, error)
	VerifySession(ctx context.Context, sessionCookie string) (Identity, error)
}

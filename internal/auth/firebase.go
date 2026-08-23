package auth

import (
	"context"
	"fmt"
	"time"

	firebaseauth "firebase.google.com/go/v4/auth"
)

type FirebaseSessionManager struct {
	client firebaseClient
	now    func() time.Time
}

func NewFirebaseSessionManager(client *firebaseauth.Client) *FirebaseSessionManager {
	return &FirebaseSessionManager{client: client, now: time.Now}
}

type firebaseClient interface {
	VerifyIDToken(ctx context.Context, idToken string) (*firebaseauth.Token, error)
	SessionCookie(ctx context.Context, idToken string, expiresIn time.Duration) (string, error)
	VerifySessionCookieAndCheckRevoked(ctx context.Context, sessionCookie string) (*firebaseauth.Token, error)
}

const maxAuthenticationAge = 5 * time.Minute

func (m *FirebaseSessionManager) CreateSession(ctx context.Context, idToken string, duration time.Duration) (string, error) {
	_, err := m.verifyRecentToken(ctx, idToken)
	if err != nil {
		return "", err
	}
	cookie, err := m.client.SessionCookie(ctx, idToken, duration)
	if err != nil {
		return "", fmt.Errorf("criar cookie de sessão: %w", err)
	}
	return cookie, nil
}

func (m *FirebaseSessionManager) VerifyRecentIDToken(ctx context.Context, idToken string) (Identity, error) {
	token, err := m.verifyRecentToken(ctx, idToken)
	if err != nil {
		return Identity{}, err
	}
	email, _ := token.Claims["email"].(string)
	if token.UID == "" || email == "" {
		return Identity{}, ErrInvalidSession
	}
	return Identity{UID: token.UID, Email: email}, nil
}

func (m *FirebaseSessionManager) verifyRecentToken(ctx context.Context, idToken string) (*firebaseauth.Token, error) {
	if idToken == "" {
		return nil, ErrInvalidSession
	}
	token, err := m.client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("validar ID token: %w", ErrInvalidSession)
	}
	authenticatedAt := time.Unix(token.AuthTime, 0)
	now := m.now()
	if token.AuthTime <= 0 || authenticatedAt.After(now) || now.Sub(authenticatedAt) > maxAuthenticationAge {
		return nil, fmt.Errorf("autenticação recente necessária: %w", ErrInvalidSession)
	}
	return token, nil
}

func (m *FirebaseSessionManager) VerifySession(ctx context.Context, sessionCookie string) (Identity, error) {
	if sessionCookie == "" {
		return Identity{}, ErrInvalidSession
	}
	token, err := m.client.VerifySessionCookieAndCheckRevoked(ctx, sessionCookie)
	if err != nil {
		return Identity{}, fmt.Errorf("validar sessão: %w", ErrInvalidSession)
	}
	email, _ := token.Claims["email"].(string)
	if token.UID == "" || email == "" {
		return Identity{}, ErrInvalidSession
	}
	return Identity{UID: token.UID, Email: email}, nil
}

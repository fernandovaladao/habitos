package auth

import (
	"context"
	"testing"
	"time"

	firebaseauth "firebase.google.com/go/v4/auth"
)

type fakeFirebaseClient struct {
	token              *firebaseauth.Token
	sessionCookieCalls int
}

func (f *fakeFirebaseClient) VerifyIDToken(context.Context, string) (*firebaseauth.Token, error) {
	return f.token, nil
}

func (f *fakeFirebaseClient) SessionCookie(context.Context, string, time.Duration) (string, error) {
	f.sessionCookieCalls++
	return "session-cookie", nil
}

func (f *fakeFirebaseClient) VerifySessionCookieAndCheckRevoked(context.Context, string) (*firebaseauth.Token, error) {
	return f.token, nil
}

func TestCreateSessionRequiresAuthenticationWithinFiveMinutes(t *testing.T) {
	now := time.Date(2026, time.August, 22, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name               string
		authenticatedAt    time.Time
		wantErr            bool
		wantSessionCookies int
	}{
		{name: "autenticação recente", authenticatedAt: now.Add(-4*time.Minute - 59*time.Second), wantSessionCookies: 1},
		{name: "limite inclusivo de cinco minutos", authenticatedAt: now.Add(-5 * time.Minute), wantSessionCookies: 1},
		{name: "autenticação antiga", authenticatedAt: now.Add(-5*time.Minute - time.Second), wantErr: true},
		{name: "auth_time ausente", authenticatedAt: time.Unix(0, 0), wantErr: true},
		{name: "auth_time no futuro", authenticatedAt: now.Add(time.Second), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeFirebaseClient{token: &firebaseauth.Token{AuthTime: test.authenticatedAt.Unix()}}
			manager := &FirebaseSessionManager{client: client, now: func() time.Time { return now }}

			_, err := manager.CreateSession(context.Background(), "id-token", SessionDuration)
			if (err != nil) != test.wantErr {
				t.Fatalf("CreateSession() erro = %v, wantErr = %v", err, test.wantErr)
			}
			if client.sessionCookieCalls != test.wantSessionCookies {
				t.Fatalf("SessionCookie() chamadas = %d, esperado %d", client.sessionCookieCalls, test.wantSessionCookies)
			}
		})
	}
}

func TestVerifyRecentIDTokenReturnsOnlyVerifiedIdentity(t *testing.T) {
	now := time.Date(2026, time.August, 23, 15, 0, 0, 0, time.UTC)
	client := &fakeFirebaseClient{token: &firebaseauth.Token{
		UID:      "firebase-user",
		AuthTime: now.Add(-time.Minute).Unix(),
		Claims:   map[string]interface{}{"email": "verified@example.com"},
	}}
	manager := &FirebaseSessionManager{client: client, now: func() time.Time { return now }}
	identity, err := manager.VerifyRecentIDToken(context.Background(), "id-token")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UID != "firebase-user" || identity.Email != "verified@example.com" {
		t.Fatalf("identidade = %#v", identity)
	}
}

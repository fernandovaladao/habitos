package accountdeletion

import (
	"context"
	"fmt"

	firebaseauth "firebase.google.com/go/v4/auth"
	"habitos/internal/auth"
)

type FirebaseAccountStore struct{ client *firebaseauth.Client }

func NewFirebaseAccountStore(client *firebaseauth.Client) *FirebaseAccountStore {
	return &FirebaseAccountStore{client: client}
}

func (s *FirebaseAccountStore) RevokeTokens(ctx context.Context, uid string) error {
	err := s.client.RevokeRefreshTokens(ctx, uid)
	if firebaseauth.IsUserNotFound(err) {
		return auth.ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("revogar sessões: %w", err)
	}
	return nil
}

func (s *FirebaseAccountStore) DeleteUser(ctx context.Context, uid string) error {
	err := s.client.DeleteUser(ctx, uid)
	if firebaseauth.IsUserNotFound(err) {
		return auth.ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("excluir identidade: %w", err)
	}
	return nil
}

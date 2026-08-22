package firebaseadmin

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
)

type Clients struct {
	Auth      *firebaseauth.Client
	Firestore *firestore.Client
}

func New(ctx context.Context, projectID string) (*Clients, error) {
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("inicializar Firebase Admin: %w", err)
	}
	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("inicializar Firebase Auth: %w", err)
	}
	firestoreClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("inicializar Firestore: %w", err)
	}
	return &Clients{Auth: authClient, Firestore: firestoreClient}, nil
}

func (c *Clients) Close() error {
	return c.Firestore.Close()
}

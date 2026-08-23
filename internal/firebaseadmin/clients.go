package firebaseadmin

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	cloudstorage "cloud.google.com/go/storage"
	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
)

type Clients struct {
	Auth      *firebaseauth.Client
	Firestore *firestore.Client
	Storage   *cloudstorage.BucketHandle
}

func New(ctx context.Context, projectID, storageBucket string) (*Clients, error) {
	if storageBucket == "" {
		return nil, fmt.Errorf("bucket do Firebase Storage não informado")
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID, StorageBucket: storageBucket})
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
	storageClient, err := app.Storage(ctx)
	if err != nil {
		return nil, fmt.Errorf("inicializar Firebase Storage: %w", err)
	}
	bucket, err := storageClient.DefaultBucket()
	if err != nil {
		return nil, fmt.Errorf("inicializar bucket: %w", err)
	}
	return &Clients{Auth: authClient, Firestore: firestoreClient, Storage: bucket}, nil
}

func (c *Clients) Close() error {
	return c.Firestore.Close()
}

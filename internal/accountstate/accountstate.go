package accountstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const CollectionName = "accountDeletions"

var ErrDeleting = errors.New("exclusão da conta em andamento")

func DocumentID(uid string) string {
	sum := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(sum[:])
}

func Document(client *firestore.Client, uid string) *firestore.DocumentRef {
	return client.Collection(CollectionName).Doc(DocumentID(uid))
}

func AssertActiveTransaction(tx *firestore.Transaction, client *firestore.Client, uid string) error {
	_, err := tx.Get(Document(client, uid))
	if err == nil {
		return ErrDeleting
	}
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}

func IsDeleting(ctx context.Context, client *firestore.Client, uid string) (bool, error) {
	_, err := Document(client, uid).Get(ctx)
	if err == nil {
		return true, nil
	}
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	return false, err
}

type Checker interface {
	IsDeleting(context.Context, string) (bool, error)
}

type FirestoreChecker struct{ client *firestore.Client }

func NewFirestoreChecker(client *firestore.Client) *FirestoreChecker {
	return &FirestoreChecker{client: client}
}

func (c *FirestoreChecker) IsDeleting(ctx context.Context, uid string) (bool, error) {
	return IsDeleting(ctx, c.client, uid)
}

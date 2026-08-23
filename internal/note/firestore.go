package note

import (
	"cloud.google.com/go/firestore"
	"context"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"habitos/internal/accountstate"
	"sort"
	"time"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}
func (r *FirestoreRepository) NewID() string { return r.client.Collection("notes").NewDoc().ID }
func (r *FirestoreRepository) Create(ctx context.Context, value Note) (Note, error) {
	doc := r.client.Collection("notes").Doc(value.ID)
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
		return tx.Create(doc, value)
	})
	if err != nil {
		return Note{}, fmt.Errorf("criar nota: %w", err)
	}
	return value, nil
}
func (r *FirestoreRepository) Get(ctx context.Context, owner, id string) (Note, error) {
	snapshot, err := r.client.Collection("notes").Doc(id).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, err
	}
	var value Note
	if err := snapshot.DataTo(&value); err != nil {
		return Note{}, err
	}
	if value.OwnerUID != owner {
		return Note{}, ErrNotFound
	}
	return value, nil
}
func (r *FirestoreRepository) ListByHabit(ctx context.Context, owner, habitID string) ([]Note, error) {
	docs, err := r.client.Collection("notes").Where("habitId", "==", habitID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var values []Note
	for _, doc := range docs {
		var value Note
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.OwnerUID == owner {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.After(values[j].CreatedAt) })
	return values, nil
}
func (r *FirestoreRepository) Update(ctx context.Context, owner, id, content string, now time.Time) (Note, error) {
	doc := r.client.Collection("notes").Doc(id)
	var result Note
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, owner); err != nil {
			return err
		}
		snapshot, err := tx.Get(doc)
		if status.Code(err) == codes.NotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := snapshot.DataTo(&result); err != nil {
			return err
		}
		if result.OwnerUID != owner {
			return ErrNotFound
		}
		result.Content = content
		result.UpdatedAt = now
		return tx.Set(doc, result)
	})
	if err != nil {
		return Note{}, fmt.Errorf("editar nota: %w", err)
	}
	return result, nil
}
func (r *FirestoreRepository) Delete(ctx context.Context, owner, id string) error {
	doc := r.client.Collection("notes").Doc(id)
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, owner); err != nil {
			return err
		}
		snapshot, err := tx.Get(doc)
		if status.Code(err) == codes.NotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var value Note
		if err := snapshot.DataTo(&value); err != nil {
			return err
		}
		if value.OwnerUID != owner {
			return ErrNotFound
		}
		return tx.Delete(doc)
	})
	if err != nil {
		return fmt.Errorf("excluir nota: %w", err)
	}
	return nil
}

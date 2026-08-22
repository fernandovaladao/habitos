package habit

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}
func (r *FirestoreRepository) NewID() string { return r.client.Collection("habits").NewDoc().ID }

func (r *FirestoreRepository) Create(ctx context.Context, value Habit, version ScheduleVersion) error {
	doc := r.client.Collection("habits").Doc(value.ID)
	versionDoc := doc.Collection("scheduleVersions").Doc(version.ID)
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := tx.Create(doc, value); err != nil {
			return err
		}
		return tx.Create(versionDoc, version)
	})
	if err != nil {
		return fmt.Errorf("criar hábito: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) Get(ctx context.Context, ownerUID, id string) (Habit, error) {
	snapshot, err := r.client.Collection("habits").Doc(id).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Habit{}, ErrNotFound
	}
	if err != nil {
		return Habit{}, fmt.Errorf("buscar hábito: %w", err)
	}
	var value Habit
	if err := snapshot.DataTo(&value); err != nil {
		return Habit{}, fmt.Errorf("decodificar hábito: %w", err)
	}
	if value.OwnerUID != ownerUID || value.DeletedAt != nil {
		return Habit{}, ErrNotFound
	}
	return value, nil
}

func (r *FirestoreRepository) List(ctx context.Context, ownerUID string) ([]Habit, error) {
	iteratorDocs := r.client.Collection("habits").Where("ownerUid", "==", ownerUID).Documents(ctx)
	defer iteratorDocs.Stop()
	var values []Habit
	for {
		snapshot, err := iteratorDocs.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("listar hábitos: %w", err)
		}
		var value Habit
		if err := snapshot.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decodificar hábito: %w", err)
		}
		if value.DeletedAt == nil {
			values = append(values, value)
		}
	}
	return values, nil
}

func (r *FirestoreRepository) Update(ctx context.Context, value Habit, version *ScheduleVersion) error {
	doc := r.client.Collection("habits").Doc(value.ID)
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(doc)
		if status.Code(err) == codes.NotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var persisted Habit
		if err := snapshot.DataTo(&persisted); err != nil {
			return err
		}
		if persisted.OwnerUID != value.OwnerUID || persisted.DeletedAt != nil {
			return ErrNotFound
		}
		if err := tx.Set(doc, value); err != nil {
			return err
		}
		if version != nil {
			if version.OwnerUID != persisted.OwnerUID || version.HabitID != persisted.ID {
				return ErrNotFound
			}
			versionDoc := doc.Collection("scheduleVersions").Doc(version.ID)
			if persisted.PendingScheduleVersionID == version.ID && persisted.ScheduleEffectiveAt.Equal(version.EffectiveAt) {
				return tx.Set(versionDoc, *version)
			}
			return tx.Create(versionDoc, *version)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("atualizar hábito: %w", err)
	}
	return nil
}

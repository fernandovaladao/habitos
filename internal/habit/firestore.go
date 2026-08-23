package habit

import (
	"context"
	"fmt"
	"sort"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"habitos/internal/accountstate"
	"habitos/internal/gamification"
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
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
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

func (r *FirestoreRepository) ListScheduleVersions(ctx context.Context, ownerUID, habitID string) ([]ScheduleVersion, error) {
	if _, err := r.Get(ctx, ownerUID, habitID); err != nil {
		return nil, err
	}
	docs, err := r.client.Collection("habits").Doc(habitID).Collection("scheduleVersions").Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listar versões de agenda: %w", err)
	}
	values := make([]ScheduleVersion, 0, len(docs))
	for _, doc := range docs {
		var value ScheduleVersion
		if err := doc.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decodificar versão de agenda: %w", err)
		}
		if value.OwnerUID != ownerUID {
			return nil, ErrNotFound
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].EffectiveDate < values[j].EffectiveDate })
	return values, nil
}

func (r *FirestoreRepository) Update(ctx context.Context, value Habit, version *ScheduleVersion) error {
	doc := r.client.Collection("habits").Doc(value.ID)
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
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
		var streakDoc *firestore.DocumentRef
		var streakValue gamification.Streak
		resetStreak := value.ReactivatedAt != nil && (persisted.ReactivatedAt == nil || !persisted.ReactivatedAt.Equal(*value.ReactivatedAt))
		if resetStreak {
			streakDoc = r.client.Collection("habitStreaks").Doc(value.ID)
			streakValue = gamification.Streak{HabitID: value.ID, OwnerUID: value.OwnerUID}
			streakSnapshot, streakErr := tx.Get(streakDoc)
			if streakErr == nil {
				if err := streakSnapshot.DataTo(&streakValue); err != nil {
					return err
				}
				if streakValue.OwnerUID != value.OwnerUID {
					return ErrNotFound
				}
			} else if status.Code(streakErr) != codes.NotFound {
				return streakErr
			}
			streakValue.CurrentStreak = 0
			streakValue.LastScheduledExecutionDate = ""
			streakValue.UpdatedAt = value.UpdatedAt
		}
		if err := tx.Set(doc, value); err != nil {
			return err
		}
		if resetStreak {
			if err := tx.Set(streakDoc, streakValue); err != nil {
				return err
			}
		}
		if version != nil {
			if version.OwnerUID != persisted.OwnerUID || version.HabitID != persisted.ID {
				return ErrNotFound
			}
			versionDoc := doc.Collection("scheduleVersions").Doc(version.ID)
			if persisted.PendingScheduleVersionID == version.ID && persisted.ScheduleEffectiveDate == version.EffectiveDate {
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

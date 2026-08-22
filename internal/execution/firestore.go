package execution

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"habitos/internal/habit"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}
func (r *FirestoreRepository) NewID() string { return r.client.Collection("executions").NewDoc().ID }

func (r *FirestoreRepository) Ensure(ctx context.Context, value Execution, key string) (Execution, error) {
	habitDoc := r.client.Collection("habits").Doc(value.HabitID)
	uniqueDoc := r.client.Collection("executionUniqueness").Doc(key)
	executionDoc := r.client.Collection("executions").Doc(value.ID)
	result := value
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		habitSnapshot, err := tx.Get(habitDoc)
		if status.Code(err) == codes.NotFound {
			return habit.ErrNotFound
		}
		if err != nil {
			return err
		}
		var owner habit.Habit
		if err := habitSnapshot.DataTo(&owner); err != nil {
			return err
		}
		if owner.OwnerUID != value.OwnerUID || owner.DeletedAt != nil || owner.Status != habit.StatusActive {
			return habit.ErrNotFound
		}
		uniqueSnapshot, err := tx.Get(uniqueDoc)
		if err == nil {
			var pointer struct {
				ExecutionID string `firestore:"executionId"`
				OwnerUID    string `firestore:"ownerUid"`
			}
			if err := uniqueSnapshot.DataTo(&pointer); err != nil {
				return err
			}
			if pointer.OwnerUID != value.OwnerUID {
				return ErrNotFound
			}
			existing, err := tx.Get(r.client.Collection("executions").Doc(pointer.ExecutionID))
			if err != nil {
				return err
			}
			return existing.DataTo(&result)
		}
		if status.Code(err) != codes.NotFound {
			return err
		}
		if err := tx.Create(executionDoc, value); err != nil {
			return err
		}
		return tx.Create(uniqueDoc, map[string]any{"executionId": value.ID, "ownerUid": value.OwnerUID, "habitId": value.HabitID, "scheduledDate": value.ScheduledDate})
	})
	if err != nil {
		return Execution{}, fmt.Errorf("materializar execução: %w", err)
	}
	return result, nil
}

func (r *FirestoreRepository) Get(ctx context.Context, ownerUID, id string) (Execution, error) {
	snapshot, err := r.client.Collection("executions").Doc(id).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Execution{}, ErrNotFound
	}
	if err != nil {
		return Execution{}, fmt.Errorf("buscar execução: %w", err)
	}
	var value Execution
	if err := snapshot.DataTo(&value); err != nil {
		return Execution{}, fmt.Errorf("decodificar execução: %w", err)
	}
	if value.OwnerUID != ownerUID {
		return Execution{}, ErrNotFound
	}
	return value, nil
}

func (r *FirestoreRepository) ListByHabit(ctx context.Context, ownerUID, habitID, beforeDate string, limit int) ([]Execution, error) {
	docs, err := r.client.Collection("executions").Where("habitId", "==", habitID).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listar execuções: %w", err)
	}
	var values []Execution
	for _, doc := range docs {
		var value Execution
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.OwnerUID == ownerUID && (beforeDate == "" || value.ScheduledDate < beforeDate) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ScheduledDate > values[j].ScheduledDate })
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (r *FirestoreRepository) ApplyResult(ctx context.Context, ownerUID, id string, resultStatus Status, achieved int64, now time.Time) (Execution, error) {
	doc := r.client.Collection("executions").Doc(id)
	var result Execution
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
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
		if result.OwnerUID != ownerUID {
			return ErrNotFound
		}
		if result.ClosedAt != nil || !now.Before(result.RegistrationDeadline) {
			return ErrClosed
		}
		if result.Status == resultStatus && result.AchievedHundredths == achieved {
			return nil
		}
		result.Status = resultStatus
		result.AchievedHundredths = achieved
		if resultStatus == StatusNotDone {
			result.PerformedAt = nil
		} else if result.PerformedAt == nil {
			performed := now
			result.PerformedAt = &performed
		}
		result.UpdatedAt = now
		return tx.Set(doc, result)
	})
	if err != nil {
		return Execution{}, fmt.Errorf("registrar execução: %w", err)
	}
	return result, nil
}

func (r *FirestoreRepository) CloseExpired(ctx context.Context, ownerUID, habitID string, now time.Time) error {
	docs, err := r.client.Collection("executions").Where("habitId", "==", habitID).Documents(ctx).GetAll()
	if err != nil {
		return fmt.Errorf("buscar execuções vencidas: %w", err)
	}
	for _, snapshot := range docs {
		var candidate Execution
		if err := snapshot.DataTo(&candidate); err != nil {
			return err
		}
		if candidate.OwnerUID != ownerUID || candidate.ClosedAt != nil || now.Before(candidate.RegistrationDeadline) {
			continue
		}
		doc := snapshot.Ref
		err = r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			current, err := tx.Get(doc)
			if err != nil {
				return err
			}
			var value Execution
			if err := current.DataTo(&value); err != nil {
				return err
			}
			if value.OwnerUID != ownerUID || value.HabitID != habitID {
				return ErrNotFound
			}
			if value.ClosedAt != nil || now.Before(value.RegistrationDeadline) {
				return nil
			}
			if value.Status == StatusPending {
				value.Status = StatusNotDone
				value.AchievedHundredths = 0
				value.PerformedAt = nil
			}
			closed := value.RegistrationDeadline
			value.ClosedAt = &closed
			value.UpdatedAt = now
			return tx.Set(doc, value)
		})
		if err != nil {
			return fmt.Errorf("fechar execução: %w", err)
		}
	}
	return nil
}

func (r *FirestoreRepository) Cursor(ctx context.Context, ownerUID, habitID string) (string, error) {
	snapshot, err := r.client.Collection("habitOccurrenceCursors").Doc(habitID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("buscar cursor: %w", err)
	}
	var value struct {
		OwnerUID string `firestore:"ownerUid"`
		Date     string `firestore:"materializedThroughDate"`
	}
	if err := snapshot.DataTo(&value); err != nil {
		return "", err
	}
	if value.OwnerUID != ownerUID {
		return "", ErrNotFound
	}
	return value.Date, nil
}
func (r *FirestoreRepository) AdvanceCursor(ctx context.Context, ownerUID, habitID, date string, now time.Time) error {
	doc := r.client.Collection("habitOccurrenceCursors").Doc(habitID)
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(doc)
		if err == nil {
			var value struct {
				OwnerUID string `firestore:"ownerUid"`
				Date     string `firestore:"materializedThroughDate"`
			}
			if err := snapshot.DataTo(&value); err != nil {
				return err
			}
			if value.OwnerUID != ownerUID {
				return ErrNotFound
			}
			if value.Date >= date {
				return nil
			}
			return tx.Set(doc, map[string]any{"ownerUid": ownerUID, "habitId": habitID, "materializedThroughDate": date, "updatedAt": now})
		}
		if status.Code(err) != codes.NotFound {
			return err
		}
		return tx.Create(doc, map[string]any{"ownerUid": ownerUID, "habitId": habitID, "materializedThroughDate": date, "updatedAt": now})
	})
	if err != nil {
		return fmt.Errorf("avançar cursor: %w", err)
	}
	return nil
}

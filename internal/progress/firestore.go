package progress

import (
	"context"
	"fmt"
	"sort"

	"cloud.google.com/go/firestore"
	"habitos/internal/execution"
	"habitos/internal/gamification"
	"habitos/internal/habit"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}

func (r *FirestoreRepository) Executions(ctx context.Context, ownerUID, startDate, endDate string) ([]execution.Execution, error) {
	docs, err := r.client.Collection("executions").
		Where("ownerUid", "==", ownerUID).
		Where("scheduledDate", ">=", startDate).
		Where("scheduledDate", "<=", endDate).
		OrderBy("scheduledDate", firestore.Asc).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listar execuções de progresso: %w", err)
	}
	values := make([]execution.Execution, 0, len(docs))
	for _, doc := range docs {
		var value execution.Execution
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.OwnerUID != ownerUID {
			return nil, execution.ErrNotFound
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *FirestoreRepository) BonusAwards(ctx context.Context, ownerUID, startDate, endDate string) ([]gamification.BonusAward, error) {
	docs, err := r.client.Collection("habitBonusAwards").
		Where("ownerUid", "==", ownerUID).
		Where("triggerScheduledDate", ">=", startDate).
		Where("triggerScheduledDate", "<=", endDate).
		OrderBy("triggerScheduledDate", firestore.Asc).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listar bônus de progresso: %w", err)
	}
	values := make([]gamification.BonusAward, 0, len(docs))
	for _, doc := range docs {
		var value gamification.BonusAward
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.OwnerUID != ownerUID {
			return nil, execution.ErrNotFound
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *FirestoreRepository) Streaks(ctx context.Context, ownerUID string) ([]gamification.Streak, error) {
	docs, err := r.client.Collection("habitStreaks").Where("ownerUid", "==", ownerUID).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listar sequências de progresso: %w", err)
	}
	values := make([]gamification.Streak, 0, len(docs))
	for _, doc := range docs {
		var value gamification.Streak
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.OwnerUID != ownerUID {
			return nil, execution.ErrNotFound
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *FirestoreRepository) Achievements(ctx context.Context, ownerUID string) ([]gamification.UserAchievement, error) {
	docs, err := r.client.Collection("userAchievements").Where("ownerUid", "==", ownerUID).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listar conquistas de progresso: %w", err)
	}
	values := make([]gamification.UserAchievement, 0, len(docs))
	for _, doc := range docs {
		var value gamification.UserAchievement
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.OwnerUID != ownerUID {
			return nil, execution.ErrNotFound
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Milestone < values[j].Milestone })
	return values, nil
}

func (r *FirestoreRepository) Habits(ctx context.Context, ownerUID string, habitIDs []string) (map[string]HabitDescriptor, error) {
	result := make(map[string]HabitDescriptor, len(habitIDs))
	if len(habitIDs) == 0 {
		return result, nil
	}
	refs := make([]*firestore.DocumentRef, len(habitIDs))
	for index, id := range habitIDs {
		refs[index] = r.client.Collection("habits").Doc(id)
	}
	docs, err := r.client.GetAll(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("carregar hábitos de progresso: %w", err)
	}
	for index, doc := range docs {
		if !doc.Exists() {
			result[habitIDs[index]] = HabitDescriptor{ID: habitIDs[index], Deleted: true}
			continue
		}
		var value habit.Habit
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.OwnerUID != ownerUID {
			return nil, habit.ErrNotFound
		}
		result[value.ID] = HabitDescriptor{ID: value.ID, Title: value.Title, Status: value.Status, Deleted: value.DeletedAt != nil}
	}
	return result, nil
}

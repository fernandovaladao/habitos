package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"habitos/internal/accountstate"
	"habitos/internal/gamification"
	"habitos/internal/habit"
	"habitos/internal/profile"
	"habitos/internal/ranking"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}
func (r *FirestoreRepository) NewID() string { return r.client.Collection("executions").NewDoc().ID }

func (r *FirestoreRepository) Ensure(ctx context.Context, value Execution, key string) (Execution, error) {
	habitDoc := r.client.Collection("habits").Doc(value.HabitID)
	uniqueDoc := r.client.Collection("executionUniqueness").Doc(key)
	result := value
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
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
		if err := r.reconcileTransaction(ctx, tx, value.OwnerUID, value.HabitID, value.UpdatedAt, &value); err != nil {
			return err
		}
		result = value
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
		if err := accountstate.AssertActiveTransaction(tx, r.client, ownerUID); err != nil {
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
		if result.OwnerUID != ownerUID {
			return ErrNotFound
		}
		if result.ClosedAt != nil || !now.Before(result.RegistrationDeadline) {
			return ErrClosed
		}
		identical := result.Status == resultStatus && result.AchievedHundredths == achieved
		if identical && !identicalResultNeedsReconciliation(result) {
			return nil
		}
		if !identical {
			result.Status = resultStatus
			result.AchievedHundredths = achieved
			if resultStatus == StatusNotDone {
				result.PerformedAt = nil
			} else if result.PerformedAt == nil {
				performed := now
				result.PerformedAt = &performed
			}
			result.UpdatedAt = now
		}
		return r.reconcileTransaction(ctx, tx, ownerUID, result.HabitID, now, &result)
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
			if err := accountstate.AssertActiveTransaction(tx, r.client, ownerUID); err != nil {
				return err
			}
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
			return r.reconcileTransaction(ctx, tx, ownerUID, habitID, now, &value)
		})
		if err != nil {
			return fmt.Errorf("fechar execução: %w", err)
		}
	}
	return nil
}

func (r *FirestoreRepository) ReconcileHabit(ctx context.Context, ownerUID, habitID string, now time.Time) error {
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, ownerUID); err != nil {
			return err
		}
		return r.reconcileTransaction(ctx, tx, ownerUID, habitID, now, nil)
	})
	if err != nil {
		return fmt.Errorf("reconciliar gamificação: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) Streak(ctx context.Context, ownerUID, habitID string) (gamification.Streak, error) {
	snapshot, err := r.client.Collection("habitStreaks").Doc(habitID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return gamification.Streak{HabitID: habitID, OwnerUID: ownerUID}, nil
	}
	if err != nil {
		return gamification.Streak{}, fmt.Errorf("buscar sequência: %w", err)
	}
	var value gamification.Streak
	if err := snapshot.DataTo(&value); err != nil {
		return gamification.Streak{}, err
	}
	if value.OwnerUID != ownerUID {
		return gamification.Streak{}, ErrNotFound
	}
	return value, nil
}

func (r *FirestoreRepository) Achievements(ctx context.Context, ownerUID string) ([]gamification.UserAchievement, error) {
	docs, err := r.client.Collection("userAchievements").Where("ownerUid", "==", ownerUID).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listar conquistas: %w", err)
	}
	values := make([]gamification.UserAchievement, 0, len(docs))
	for _, doc := range docs {
		var value gamification.UserAchievement
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Milestone < values[j].Milestone })
	return values, nil
}

type transactionExecution struct {
	ref   *firestore.DocumentRef
	value Execution
	dirty bool
}

func (r *FirestoreRepository) reconcileTransaction(ctx context.Context, tx *firestore.Transaction, ownerUID, habitID string, now time.Time, replacement *Execution) error {
	habitDoc := r.client.Collection("habits").Doc(habitID)
	habitSnapshot, err := tx.Get(habitDoc)
	if status.Code(err) == codes.NotFound {
		return habit.ErrNotFound
	}
	if err != nil {
		return err
	}
	var ownerHabit habit.Habit
	if err := habitSnapshot.DataTo(&ownerHabit); err != nil {
		return err
	}
	if ownerHabit.OwnerUID != ownerUID {
		return habit.ErrNotFound
	}

	userDoc := r.client.Collection("users").Doc(ownerUID)
	userSnapshot, err := tx.Get(userDoc)
	if status.Code(err) == codes.NotFound {
		return profile.ErrNotFound
	}
	if err != nil {
		return err
	}
	var user profile.Profile
	if err := userSnapshot.DataTo(&user); err != nil {
		return err
	}

	streakDoc := r.client.Collection("habitStreaks").Doc(habitID)
	streak := gamification.Streak{HabitID: habitID, OwnerUID: ownerUID}
	streakSnapshot, streakErr := tx.Get(streakDoc)
	streakExists := streakErr == nil
	if streakErr == nil {
		if err := streakSnapshot.DataTo(&streak); err != nil {
			return err
		}
		if streak.OwnerUID != ownerUID {
			return ErrNotFound
		}
	} else if status.Code(streakErr) != codes.NotFound {
		return streakErr
	}

	iterator := tx.Documents(r.client.Collection("executions").Where("habitId", "==", habitID))
	docs, err := iterator.GetAll()
	if err != nil {
		return err
	}
	executions := make([]transactionExecution, 0, len(docs))
	foundReplacement := replacement == nil
	oldPoints := int64(0)
	for _, doc := range docs {
		var value Execution
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if value.OwnerUID != ownerUID {
			return ErrNotFound
		}
		oldPoints += int64(value.PointsAwarded)
		if replacement != nil && value.ID == replacement.ID {
			value = *replacement
			foundReplacement = true
		}
		executions = append(executions, transactionExecution{ref: doc.Ref, value: value})
	}
	if !foundReplacement {
		executions = append(executions, transactionExecution{ref: r.client.Collection("executions").Doc(replacement.ID), value: *replacement, dirty: true})
	}

	awardExists := make(map[int]bool, len(gamification.Milestones))
	achievementExists := make(map[int]bool, len(gamification.Milestones))
	for _, milestone := range gamification.Milestones {
		awardDoc := r.client.Collection("habitBonusAwards").Doc(deterministicID(ownerUID, habitID, fmt.Sprint(milestone.Value)))
		if _, err := tx.Get(awardDoc); err == nil {
			awardExists[milestone.Value] = true
		} else if status.Code(err) != codes.NotFound {
			return err
		}
		achievementDoc := r.client.Collection("userAchievements").Doc(deterministicID(ownerUID, milestone.Code))
		if _, err := tx.Get(achievementDoc); err == nil {
			achievementExists[milestone.Value] = true
		} else if status.Code(err) != codes.NotFound {
			return err
		}
	}

	sort.Slice(executions, func(i, j int) bool { return executions[i].value.ScheduledDate < executions[j].value.ScheduledDate })
	sequenceRecords := make([]gamification.SequenceRecord, len(executions))
	for index, item := range executions {
		sequenceRecords[index] = gamification.SequenceRecord{ScheduledDate: item.value.ScheduledDate, Status: string(item.value.Status)}
	}
	resetDate := ""
	if ownerHabit.ReactivatedAt != nil {
		resetDate = ownerHabit.OccurrencesResumeDate
	}
	projection := gamification.ProjectSequence(sequenceRecords, streak.BestStreak, resetDate)
	newAwards := make([]gamification.BonusAward, 0)
	newAchievements := make([]gamification.UserAchievement, 0)
	for index := range executions {
		value := &executions[index].value
		points, err := pointsFor(*value)
		if err != nil {
			return err
		}
		step := projection.Steps[index]
		before, after := step.Before, step.After
		if step.Confirmed {
			for _, milestone := range gamification.MilestonesReached(before, after) {
				if awardExists[milestone.Value] {
					continue
				}
				awardExists[milestone.Value] = true
				awardID := deterministicID(ownerUID, habitID, fmt.Sprint(milestone.Value))
				newAwards = append(newAwards, gamification.BonusAward{ID: awardID, OwnerUID: ownerUID, HabitID: habitID, Milestone: milestone.Value, Points: milestone.Bonus, TriggerExecutionID: value.ID, TriggerScheduledDate: value.ScheduledDate, AwardedAt: now})
				if !achievementExists[milestone.Value] {
					achievementExists[milestone.Value] = true
					newAchievements = append(newAchievements, gamification.UserAchievement{ID: deterministicID(ownerUID, milestone.Code), OwnerUID: ownerUID, AchievementCode: milestone.Code, Name: milestone.Name, Description: milestone.Description, Milestone: milestone.Value, BonusWhenApplicable: milestone.Bonus, SourceHabitID: habitID, SourceExecutionID: value.ID, UnlockedAt: now})
				}
			}
		}
		changed := value.PointsAwarded != points || value.StreakBefore != before || value.StreakAfter != after || value.ScoredAt == nil
		value.PointsAwarded = points
		value.StreakBefore = before
		value.StreakAfter = after
		if changed {
			scored := now
			value.ScoredAt = &scored
			executions[index].dirty = true
		}
	}

	newPoints := int64(0)
	for _, item := range executions {
		newPoints += int64(item.value.PointsAwarded)
	}
	bonusDelta := int64(0)
	for _, award := range newAwards {
		bonusDelta += award.Points
	}
	totalDelta := newPoints - oldPoints + bonusDelta
	if totalDelta != 0 {
		user.TotalPoints += totalDelta
		reached := now
		user.TotalPointsReachedAt = &reached
		if err := ranking.ReconcileTransaction(tx, r.client, ranking.ProjectionInput{
			UID: user.UID, Nickname: user.Nickname, RankingOptIn: user.RankingOptIn, ProfileComplete: user.ProfileComplete,
			TotalPoints: user.TotalPoints, TotalPointsReachedAt: user.TotalPointsReachedAt, CreatedAt: user.CreatedAt, UpdatedAt: now,
		}); err != nil {
			return err
		}
	}

	milestones := make([]int, 0)
	for _, milestone := range gamification.Milestones {
		if awardExists[milestone.Value] {
			milestones = append(milestones, milestone.Value)
		}
	}
	streakChanged := !streakExists || streak.CurrentStreak != projection.Current || streak.BestStreak != projection.Best || streak.LastScheduledExecutionDate != projection.LastConfirmedDate || !sameInts(streak.MilestonesAwarded, milestones)
	streak.CurrentStreak = projection.Current
	streak.BestStreak = projection.Best
	streak.LastScheduledExecutionDate = projection.LastConfirmedDate
	streak.MilestonesAwarded = milestones
	if streakChanged {
		streak.UpdatedAt = now
	}

	for _, item := range executions {
		if item.dirty || replacement != nil && item.value.ID == replacement.ID {
			if err := tx.Set(item.ref, item.value); err != nil {
				return err
			}
		}
		if replacement != nil && item.value.ID == replacement.ID {
			*replacement = item.value
		}
	}
	if streakChanged {
		if err := tx.Set(streakDoc, streak); err != nil {
			return err
		}
	}
	if totalDelta != 0 {
		if err := tx.Set(userDoc, user); err != nil {
			return err
		}
	}
	for _, award := range newAwards {
		if err := tx.Create(r.client.Collection("habitBonusAwards").Doc(award.ID), award); err != nil {
			return err
		}
	}
	for _, achievement := range newAchievements {
		if err := tx.Create(r.client.Collection("userAchievements").Doc(achievement.ID), achievement); err != nil {
			return err
		}
	}
	return nil
}

func pointsFor(value Execution) (int, error) {
	switch value.Status {
	case StatusCompleted:
		return 10, nil
	case StatusPartial:
		return gamification.QuantitativePoints(value.AchievedHundredths, value.TargetHundredthsSnapshot)
	default:
		return 0, nil
	}
}

func identicalResultNeedsReconciliation(value Execution) bool {
	return value.ScoredAt == nil
}

func deterministicID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func sameInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
		if err := accountstate.AssertActiveTransaction(tx, r.client, ownerUID); err != nil {
			return err
		}
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

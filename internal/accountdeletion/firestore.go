package accountdeletion

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"habitos/internal/accountstate"
	"habitos/internal/ranking"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}

func (r *FirestoreRepository) Begin(ctx context.Context, uid string, now time.Time) (State, error) {
	ref := accountstate.Document(r.client, uid)
	state := State{Stage: StageNotes, StartedAt: now, UpdatedAt: now}
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if err == nil {
			if err := snapshot.DataTo(&state); err != nil {
				return err
			}
		} else if status.Code(err) != codes.NotFound {
			return err
		}
		rankingRef := r.client.Collection(ranking.CollectionName).Doc(uid)
		if _, err := tx.Get(rankingRef); err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		if err := tx.Set(ref, state); err != nil {
			return err
		}
		return tx.Delete(rankingRef)
	})
	if err != nil {
		return State{}, fmt.Errorf("iniciar exclusão: %w", err)
	}
	return state, nil
}

func (r *FirestoreRepository) State(ctx context.Context, uid string) (State, error) {
	snapshot, err := accountstate.Document(r.client, uid).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return State{}, ErrNotStarted
	}
	if err != nil {
		return State{}, fmt.Errorf("consultar exclusão: %w", err)
	}
	var state State
	if err := snapshot.DataTo(&state); err != nil {
		return State{}, fmt.Errorf("decodificar exclusão: %w", err)
	}
	return state, nil
}

func (r *FirestoreRepository) AcquireLease(ctx context.Context, uid, leaseID string, now time.Time) (State, bool, error) {
	ref := accountstate.Document(r.client, uid)
	var state State
	acquired := false
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return ErrNotStarted
		}
		if err != nil {
			return err
		}
		if err := snapshot.DataTo(&state); err != nil {
			return err
		}
		if state.LeaseUntil != nil && now.Before(*state.LeaseUntil) {
			return nil
		}
		until := now.Add(LeaseDuration)
		state.LeaseID, state.LeaseUntil = leaseID, &until
		acquired = true
		return tx.Update(ref, []firestore.Update{{Path: "leaseId", Value: leaseID}, {Path: "leaseUntil", Value: until}})
	})
	if err != nil {
		return State{}, false, fmt.Errorf("adquirir lease de exclusão: %w", err)
	}
	return state, acquired, nil
}

func (r *FirestoreRepository) ReleaseLease(ctx context.Context, uid, leaseID string) error {
	ref := accountstate.Document(r.client, uid)
	return r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var state State
		if err := snapshot.DataTo(&state); err != nil {
			return err
		}
		if state.LeaseID != leaseID {
			return nil
		}
		return tx.Update(ref, []firestore.Update{{Path: "leaseId", Value: firestore.Delete}, {Path: "leaseUntil", Value: firestore.Delete}})
	})
}

func nextStage(stage Stage) Stage {
	stages := []Stage{StageNotes, StageUniqueness, StageExecutions, StageCursors, StageStreaks, StageBonuses, StageAchievements, StageSchedules, StageHabits, StageAvatarMedia, StageAvatarCleanup, StagePushSubscriptions, StageReminderSchedules, StageReminderDeliveries, StageProfile, StageStorage}
	for index, candidate := range stages {
		if candidate == stage && index+1 < len(stages) {
			return stages[index+1]
		}
	}
	return StageStorage
}

func stageCollection(stage Stage) string {
	return map[Stage]string{
		StageNotes: "notes", StageUniqueness: "executionUniqueness", StageExecutions: "executions",
		StageCursors: "habitOccurrenceCursors", StageStreaks: "habitStreaks", StageBonuses: "habitBonusAwards",
		StageAchievements: "userAchievements", StageHabits: "habits", StageAvatarMedia: "avatarMedia", StageAvatarCleanup: "avatarCleanup",
		StagePushSubscriptions: "pushSubscriptions", StageReminderSchedules: "reminderSchedules", StageReminderDeliveries: "reminderDeliveries",
	}[stage]
}

func (r *FirestoreRepository) DeleteBatch(ctx context.Context, uid string, stage Stage, limit int, now time.Time) (State, error) {
	if stage == StageProfile {
		batch := r.client.Batch()
		batch.Delete(r.client.Collection("users").Doc(uid))
		batch.Set(accountstate.Document(r.client, uid), map[string]any{"stage": StageStorage, "updatedAt": now}, firestore.MergeAll)
		if _, err := batch.Commit(ctx); err != nil {
			return State{}, fmt.Errorf("excluir perfil: %w", err)
		}
		return r.State(ctx, uid)
	}
	var query firestore.Query
	if stage == StageSchedules {
		query = r.client.CollectionGroup("scheduleVersions").Where("ownerUid", "==", uid).Limit(limit)
	} else {
		collection := stageCollection(stage)
		if collection == "" {
			return State{}, fmt.Errorf("estágio de exclusão inválido")
		}
		query = r.client.Collection(collection).Where("ownerUid", "==", uid).Limit(limit)
	}
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return State{}, fmt.Errorf("listar lote de exclusão: %w", err)
	}
	batch := r.client.Batch()
	for _, doc := range docs {
		batch.Delete(doc.Ref)
	}
	if len(docs) < limit {
		batch.Set(accountstate.Document(r.client, uid), map[string]any{"stage": nextStage(stage), "updatedAt": now}, firestore.MergeAll)
	} else {
		batch.Set(accountstate.Document(r.client, uid), map[string]any{"updatedAt": now}, firestore.MergeAll)
	}
	if _, err := batch.Commit(ctx); err != nil {
		return State{}, fmt.Errorf("excluir lote: %w", err)
	}
	return r.State(ctx, uid)
}

func (r *FirestoreRepository) FunctionalEmpty(ctx context.Context, uid string) (bool, error) {
	for _, ref := range []*firestore.DocumentRef{r.client.Collection(ranking.CollectionName).Doc(uid), r.client.Collection("users").Doc(uid)} {
		if _, err := ref.Get(ctx); err == nil {
			return false, nil
		} else if status.Code(err) != codes.NotFound {
			return false, err
		}
	}
	for _, collection := range []string{"notes", "executionUniqueness", "executions", "habitOccurrenceCursors", "habitStreaks", "habitBonusAwards", "userAchievements", "habits", "avatarMedia", "avatarCleanup", "pushSubscriptions", "reminderSchedules", "reminderDeliveries"} {
		docs, err := r.client.Collection(collection).Where("ownerUid", "==", uid).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return false, err
		}
		if len(docs) != 0 {
			return false, nil
		}
	}
	versions, err := r.client.CollectionGroup("scheduleVersions").Where("ownerUid", "==", uid).Limit(1).Documents(ctx).GetAll()
	return len(versions) == 0, err
}

func (r *FirestoreRepository) SetStage(ctx context.Context, uid string, stage Stage, now time.Time) (State, error) {
	_, err := accountstate.Document(r.client, uid).Set(ctx, map[string]any{"stage": stage, "updatedAt": now}, firestore.MergeAll)
	if err != nil {
		return State{}, fmt.Errorf("avançar exclusão: %w", err)
	}
	return r.State(ctx, uid)
}

func (r *FirestoreRepository) RemoveMarker(ctx context.Context, uid string) error {
	_, err := accountstate.Document(r.client, uid).Delete(ctx)
	return err
}

func (r *FirestoreRepository) ArmMarkerTTL(ctx context.Context, uid string, expiresAt time.Time) error {
	_, err := accountstate.Document(r.client, uid).Set(ctx, map[string]any{"expiresAt": expiresAt}, firestore.MergeAll)
	return err
}

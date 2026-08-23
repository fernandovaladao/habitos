package profile

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

type FirestoreRepository struct {
	client *firestore.Client
}

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}

func (r *FirestoreRepository) Ensure(ctx context.Context, candidate Profile) (Profile, error) {
	document := r.client.Collection("users").Doc(candidate.UID)
	result := candidate
	err := r.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(transaction, r.client, candidate.UID); err != nil {
			return err
		}
		snapshot, err := transaction.Get(document)
		exists := err == nil
		var legacyUpdates []firestore.Update
		if err == nil {
			if err := snapshot.DataTo(&result); err != nil {
				return fmt.Errorf("decodificar perfil: %w", err)
			}
			legacyUpdates = applyLegacyDefaults(&result, snapshot.Data())
		} else if status.Code(err) == codes.NotFound {
			result = candidate
		} else {
			return err
		}
		if err := ranking.ReconcileTransaction(transaction, r.client, rankingInput(result)); err != nil {
			return err
		}
		if !exists {
			return transaction.Create(document, candidate)
		}
		if len(legacyUpdates) > 0 {
			return transaction.Update(document, legacyUpdates)
		}
		return nil
	})
	if err != nil {
		return Profile{}, fmt.Errorf("garantir perfil: %w", err)
	}
	return result, nil
}

func applyLegacyDefaults(value *Profile, data map[string]interface{}) []firestore.Update {
	updates := make([]firestore.Update, 0, 3)
	if _, exists := data["avatarType"]; !exists {
		value.AvatarType = AvatarDefault
		updates = append(updates, firestore.Update{Path: "avatarType", Value: AvatarDefault})
	}
	if _, exists := data["reminderNotificationEnabled"]; !exists {
		value.ReminderNotificationEnabled = true
		updates = append(updates, firestore.Update{Path: "reminderNotificationEnabled", Value: true})
	}
	if _, exists := data["reminderEmailEnabled"]; !exists {
		value.ReminderEmailEnabled = true
		updates = append(updates, firestore.Update{Path: "reminderEmailEnabled", Value: true})
	}
	return updates
}

func (r *FirestoreRepository) Get(ctx context.Context, uid string) (Profile, error) {
	snapshot, err := r.client.Collection("users").Doc(uid).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("buscar perfil: %w", err)
	}
	var result Profile
	if err := snapshot.DataTo(&result); err != nil {
		return Profile{}, fmt.Errorf("decodificar perfil: %w", err)
	}
	return result, nil
}

func (r *FirestoreRepository) Update(ctx context.Context, uid string, update Update, updatedAt time.Time) (Profile, error) {
	document := r.client.Collection("users").Doc(uid)
	var result Profile
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, uid); err != nil {
			return err
		}
		snapshot, err := tx.Get(document)
		if status.Code(err) == codes.NotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := snapshot.DataTo(&result); err != nil {
			return err
		}
		result.Nickname = update.Nickname
		result.Age = update.Age
		result.WeightHundredths = update.WeightHundredths
		result.HeightHundredths = update.HeightHundredths
		result.Gender = update.Gender
		result.AvatarType = update.AvatarType
		result.Timezone = update.Timezone
		result.RankingOptIn = update.RankingOptIn
		result.ReminderNotificationEnabled = update.ReminderNotificationEnabled
		result.ReminderEmailEnabled = update.ReminderEmailEnabled
		result.ProfileComplete = true
		result.UpdatedAt = updatedAt
		if err := ranking.ReconcileTransaction(tx, r.client, rankingInput(result)); err != nil {
			return err
		}
		return tx.Set(document, result)
	})
	if err != nil {
		return Profile{}, fmt.Errorf("atualizar perfil: %w", err)
	}
	return result, nil
}

func (r *FirestoreRepository) UpdateDemographics(ctx context.Context, uid string, value Demographics, updatedAt time.Time) (Profile, error) {
	document := r.client.Collection("users").Doc(uid)
	var result Profile
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, uid); err != nil {
			return err
		}
		snapshot, err := tx.Get(document)
		if status.Code(err) == codes.NotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := snapshot.DataTo(&result); err != nil {
			return err
		}
		result.Age = value.Age
		result.WeightHundredths = value.WeightHundredths
		result.HeightHundredths = value.HeightHundredths
		result.Gender = value.Gender
		result.UpdatedAt = updatedAt
		if err := ranking.ReconcileTransaction(tx, r.client, rankingInput(result)); err != nil {
			return err
		}
		return tx.Set(document, result)
	})
	if err != nil {
		return Profile{}, fmt.Errorf("atualizar dados do perfil: %w", err)
	}
	return result, nil
}

func rankingInput(value Profile) ranking.ProjectionInput {
	return ranking.ProjectionInput{
		UID: value.UID, Nickname: value.Nickname, RankingOptIn: value.RankingOptIn, ProfileComplete: value.ProfileComplete,
		AvatarType:  value.AvatarType,
		TotalPoints: value.TotalPoints, TotalPointsReachedAt: value.TotalPointsReachedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

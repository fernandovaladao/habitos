package profile

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		snapshot, err := transaction.Get(document)
		if err == nil {
			if err := snapshot.DataTo(&result); err != nil {
				return fmt.Errorf("decodificar perfil: %w", err)
			}
			return nil
		}
		if status.Code(err) != codes.NotFound {
			return err
		}
		return transaction.Create(document, candidate)
	})
	if err != nil {
		return Profile{}, fmt.Errorf("garantir perfil: %w", err)
	}
	return result, nil
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
	_, err := document.Update(ctx, []firestore.Update{
		{Path: "nickname", Value: update.Nickname},
		{Path: "age", Value: update.Age},
		{Path: "weightHundredths", Value: update.WeightHundredths},
		{Path: "heightHundredths", Value: update.HeightHundredths},
		{Path: "gender", Value: update.Gender},
		{Path: "timezone", Value: update.Timezone},
		{Path: "rankingOptIn", Value: update.RankingOptIn},
		{Path: "profileComplete", Value: true},
		{Path: "updatedAt", Value: updatedAt},
	})
	if status.Code(err) == codes.NotFound {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("atualizar perfil: %w", err)
	}
	return r.Get(ctx, uid)
}

func (r *FirestoreRepository) UpdateDemographics(ctx context.Context, uid string, value Demographics, updatedAt time.Time) (Profile, error) {
	document := r.client.Collection("users").Doc(uid)
	_, err := document.Update(ctx, []firestore.Update{
		{Path: "age", Value: value.Age},
		{Path: "weightHundredths", Value: value.WeightHundredths},
		{Path: "heightHundredths", Value: value.HeightHundredths},
		{Path: "gender", Value: value.Gender},
		{Path: "updatedAt", Value: updatedAt},
	})
	if status.Code(err) == codes.NotFound {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("atualizar dados do perfil: %w", err)
	}
	return r.Get(ctx, uid)
}

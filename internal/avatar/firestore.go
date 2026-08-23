package avatar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"habitos/internal/accountstate"
	"habitos/internal/profile"
	"habitos/internal/ranking"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}

func (r *FirestoreRepository) ReplacePhoto(ctx context.Context, uid string, media Media, now time.Time) (string, error) {
	return r.mutate(ctx, uid, &media, "", now)
}

func (r *FirestoreRepository) RemovePhoto(ctx context.Context, uid string, internalType string, now time.Time) (string, error) {
	return r.mutate(ctx, uid, nil, internalType, now)
}

func (r *FirestoreRepository) mutate(ctx context.Context, uid string, media *Media, internalType string, now time.Time) (string, error) {
	userRef := r.client.Collection("users").Doc(uid)
	oldPath := ""
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, uid); err != nil {
			return err
		}
		snapshot, err := tx.Get(userRef)
		if status.Code(err) == codes.NotFound {
			return profile.ErrNotFound
		}
		if err != nil {
			return err
		}
		var value profile.Profile
		if err := snapshot.DataTo(&value); err != nil || value.UID != uid {
			return profile.ErrNotFound
		}
		oldPath = value.PhotoObjectPath
		oldMediaID := value.PhotoMediaID
		if internalType != "" {
			value.AvatarType = internalType
		}
		if media == nil {
			value.PhotoMediaID, value.PhotoObjectPath, value.PhotoUpdatedAt = "", "", nil
		} else {
			updated := now
			value.PhotoMediaID, value.PhotoObjectPath, value.PhotoUpdatedAt = media.ID, media.ObjectPath, &updated
		}
		value.UpdatedAt = now
		if err := ranking.ReconcileTransaction(tx, r.client, ranking.ProjectionInput{
			UID: value.UID, Nickname: value.Nickname, AvatarType: value.AvatarType, RankingOptIn: value.RankingOptIn, ProfileComplete: value.ProfileComplete,
			TotalPoints: value.TotalPoints, TotalPointsReachedAt: value.TotalPointsReachedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		}); err != nil {
			return err
		}
		if media != nil {
			if err := tx.Set(r.client.Collection("avatarMedia").Doc(media.ID), *media); err != nil {
				return err
			}
		}
		if oldMediaID != "" {
			if err := tx.Delete(r.client.Collection("avatarMedia").Doc(oldMediaID)); err != nil {
				return err
			}
		}
		if oldPath != "" && (media == nil || oldPath != media.ObjectPath) {
			cleanupID := pathID(oldPath)
			if err := tx.Set(r.client.Collection("avatarCleanup").Doc(cleanupID), map[string]interface{}{"ownerUid": uid, "objectPath": oldPath, "createdAt": now}); err != nil {
				return err
			}
		}
		return tx.Set(userRef, value)
	})
	if err != nil {
		return "", fmt.Errorf("atualizar foto do perfil: %w", err)
	}
	return oldPath, nil
}

func (r *FirestoreRepository) Media(ctx context.Context, mediaID string) (Media, error) {
	snapshot, err := r.client.Collection("avatarMedia").Doc(mediaID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Media{}, ErrNotFound
	}
	if err != nil {
		return Media{}, fmt.Errorf("buscar foto: %w", err)
	}
	var value Media
	if err := snapshot.DataTo(&value); err != nil {
		return Media{}, fmt.Errorf("decodificar foto: %w", err)
	}
	return value, nil
}

func (r *FirestoreRepository) CurrentMedia(ctx context.Context, uid string) (Media, error) {
	snapshot, err := r.client.Collection("users").Doc(uid).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Media{}, ErrNotFound
	}
	if err != nil {
		return Media{}, fmt.Errorf("buscar perfil da foto: %w", err)
	}
	var value profile.Profile
	if err := snapshot.DataTo(&value); err != nil || value.UID != uid || value.PhotoMediaID == "" {
		return Media{}, ErrNotFound
	}
	return r.Media(ctx, value.PhotoMediaID)
}

func (r *FirestoreRepository) CleanupCompleted(ctx context.Context, objectPath string) error {
	_, err := r.client.Collection("avatarCleanup").Doc(pathID(objectPath)).Delete(ctx)
	return err
}

func pathID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

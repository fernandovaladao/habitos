package accountdeletion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"habitos/internal/auth"
)

type Service struct {
	repository Repository
	objects    ObjectStore
	tokens     RecentTokenVerifier
	accounts   AccountStore
	now        func() time.Time
}

func NewService(repository Repository, objects ObjectStore, tokens RecentTokenVerifier, accounts AccountStore) *Service {
	return &Service{repository: repository, objects: objects, tokens: tokens, accounts: accounts, now: time.Now}
}

func (s *Service) Start(ctx context.Context, identity auth.Identity, confirmation, idToken string) (Result, error) {
	if identity.UID == "" || identity.Email == "" {
		return Result{}, auth.ErrInvalidSession
	}
	if confirmation != ConfirmationPhrase {
		return Result{}, ErrInvalidConfirmation
	}
	recent, err := s.tokens.VerifyRecentIDToken(ctx, idToken)
	if err != nil {
		return Result{}, auth.ErrInvalidSession
	}
	if recent.UID != identity.UID || recent.Email != identity.Email {
		return Result{}, ErrIdentityMismatch
	}
	if _, err := s.repository.Begin(ctx, identity.UID, normalized(s.now())); err != nil {
		return Result{}, err
	}
	return s.Continue(ctx, identity)
}

func (s *Service) Continue(ctx context.Context, identity auth.Identity) (Result, error) {
	if identity.UID == "" || identity.Email == "" {
		return Result{}, auth.ErrInvalidSession
	}
	leaseID, err := newLeaseID()
	if err != nil {
		return Result{}, err
	}
	now := normalized(s.now())
	state, acquired, err := s.repository.AcquireLease(ctx, identity.UID, leaseID, now)
	if err != nil {
		return Result{}, err
	}
	if !acquired {
		return Result{Stage: string(state.Stage)}, nil
	}
	defer func() { _ = s.repository.ReleaseLease(context.WithoutCancel(ctx), identity.UID, leaseID) }()
	switch state.Stage {
	case StageStorage:
		done, err := s.objects.DeleteBatch(ctx, "avatars/"+identity.UID+"/", BatchSize)
		if err != nil {
			return Result{}, err
		}
		if done {
			state, err = s.repository.SetStage(ctx, identity.UID, StageVerify, now)
		}
	case StageVerify:
		empty, err := s.repository.FunctionalEmpty(ctx, identity.UID)
		if err != nil {
			return Result{}, err
		}
		storageEmpty, err := s.objects.Empty(ctx, "avatars/"+identity.UID+"/")
		if err != nil {
			return Result{}, err
		}
		if !empty {
			state, err = s.repository.SetStage(ctx, identity.UID, StageNotes, now)
		} else if !storageEmpty {
			state, err = s.repository.SetStage(ctx, identity.UID, StageStorage, now)
		} else {
			state, err = s.repository.SetStage(ctx, identity.UID, StageAuth, now)
		}
	case StageAuth:
		if err := s.accounts.RevokeTokens(ctx, identity.UID); err != nil && !errors.Is(err, auth.ErrUserNotFound) {
			return Result{}, err
		}
		if err := s.accounts.DeleteUser(ctx, identity.UID); err != nil && !errors.Is(err, auth.ErrUserNotFound) {
			return Result{}, err
		}
		// O TTL só é armado depois que a identidade deixou de existir e é apenas
		// fallback. Sua falha não impede a tentativa de remoção imediata.
		_ = s.repository.ArmMarkerTTL(ctx, identity.UID, now.Add(MarkerTTL))
		_ = s.repository.RemoveMarker(ctx, identity.UID)
		return Result{Complete: true}, nil
	default:
		state, err = s.repository.DeleteBatch(ctx, identity.UID, state.Stage, BatchSize, now)
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Stage: string(state.Stage)}, nil
}

func normalized(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }

func newLeaseID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

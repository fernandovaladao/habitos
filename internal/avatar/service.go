package avatar

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"habitos/internal/auth"
	"habitos/internal/profile"
)

type Service struct {
	repository Repository
	objects    ObjectStore
	now        func() time.Time
	newID      func() (string, error)
}

func NewService(repository Repository, objects ObjectStore) *Service {
	return &Service{repository: repository, objects: objects, now: time.Now, newID: randomID}
}

func (s *Service) Upload(ctx context.Context, identity auth.Identity, reader io.Reader) (Media, error) {
	if identity.UID == "" {
		return Media{}, auth.ErrInvalidSession
	}
	raw, err := readLimited(reader)
	if err != nil {
		return Media{}, err
	}
	normalized, err := NormalizeImage(raw)
	if err != nil {
		return Media{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Media{}, errors.New("gerar identificador da foto")
	}
	media := Media{ID: id, OwnerUID: identity.UID, ObjectPath: "avatars/" + identity.UID + "/" + id + ".jpg", CreatedAt: normalizeTime(s.now())}
	if err := s.objects.Write(ctx, media.ObjectPath, normalized); err != nil {
		return Media{}, err
	}
	oldPath, err := s.repository.ReplacePhoto(ctx, identity.UID, media, media.CreatedAt)
	if err != nil {
		_ = s.objects.Delete(ctx, media.ObjectPath)
		return Media{}, err
	}
	s.cleanup(ctx, oldPath)
	return media, nil
}

func (s *Service) RemovePhoto(ctx context.Context, identity auth.Identity) error {
	return s.remove(ctx, identity, "")
}

func (s *Service) SelectInternal(ctx context.Context, identity auth.Identity, avatarType string) error {
	if !profile.ValidAvatarType(avatarType) {
		return profile.ErrInvalidAvatar
	}
	return s.remove(ctx, identity, avatarType)
}

func (s *Service) remove(ctx context.Context, identity auth.Identity, avatarType string) error {
	if identity.UID == "" {
		return auth.ErrInvalidSession
	}
	oldPath, err := s.repository.RemovePhoto(ctx, identity.UID, avatarType, normalizeTime(s.now()))
	if err != nil {
		return err
	}
	s.cleanup(ctx, oldPath)
	return nil
}

func (s *Service) Read(ctx context.Context, identity auth.Identity, mediaID string) ([]byte, error) {
	if identity.UID == "" {
		return nil, auth.ErrInvalidSession
	}
	var media Media
	var err error
	if mediaID == "current" {
		media, err = s.repository.CurrentMedia(ctx, identity.UID)
		mediaID = media.ID
	} else {
		media, err = s.repository.Media(ctx, mediaID)
	}
	if err != nil || media.ID != mediaID || media.OwnerUID != identity.UID || media.ObjectPath != "avatars/"+identity.UID+"/"+mediaID+".jpg" {
		return nil, ErrNotFound
	}
	return s.objects.Read(ctx, media.ObjectPath)
}

func (s *Service) cleanup(ctx context.Context, path string) {
	if path == "" {
		return
	}
	if err := s.objects.Delete(ctx, path); err == nil {
		_ = s.repository.CleanupCompleted(ctx, path)
	}
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func normalizeTime(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }

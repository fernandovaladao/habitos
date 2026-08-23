package avatar

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"habitos/internal/auth"
	"habitos/internal/profile"
)

type serviceRepository struct {
	media       map[string]Media
	oldPath     string
	replaceErr  error
	removeErr   error
	replaceCall int
	removeType  string
	cleaned     []string
}

func (r *serviceRepository) ReplacePhoto(_ context.Context, _ string, media Media, _ time.Time) (string, error) {
	r.replaceCall++
	if r.replaceErr != nil {
		return "", r.replaceErr
	}
	r.media[media.ID] = media
	return r.oldPath, nil
}
func (r *serviceRepository) RemovePhoto(_ context.Context, _ string, internal string, _ time.Time) (string, error) {
	r.removeType = internal
	return r.oldPath, r.removeErr
}
func (r *serviceRepository) Media(_ context.Context, id string) (Media, error) {
	media, ok := r.media[id]
	if !ok {
		return Media{}, ErrNotFound
	}
	return media, nil
}
func (r *serviceRepository) CurrentMedia(_ context.Context, uid string) (Media, error) {
	for _, media := range r.media {
		if media.OwnerUID == uid {
			return media, nil
		}
	}
	return Media{}, ErrNotFound
}
func (r *serviceRepository) CleanupCompleted(_ context.Context, path string) error {
	r.cleaned = append(r.cleaned, path)
	return nil
}

type serviceStore struct {
	values    map[string][]byte
	writeErr  error
	deleteErr error
	deleted   []string
}

func (s *serviceStore) Write(_ context.Context, path string, value []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.values[path] = append([]byte(nil), value...)
	return nil
}
func (s *serviceStore) Read(_ context.Context, path string) ([]byte, error) {
	value, ok := s.values[path]
	if !ok {
		return nil, ErrNotFound
	}
	return value, nil
}
func (s *serviceStore) Delete(_ context.Context, path string) error {
	s.deleted = append(s.deleted, path)
	if s.deleteErr == nil {
		delete(s.values, path)
	}
	return s.deleteErr
}

func TestUploadCompensatesFirestoreFailure(t *testing.T) {
	repository := &serviceRepository{media: map[string]Media{}, replaceErr: errors.New("firestore indisponível")}
	store := &serviceStore{values: map[string][]byte{}}
	service := NewService(repository, store)
	service.newID = func() (string, error) { return "nova-foto", nil }
	_, err := service.Upload(context.Background(), auth.Identity{UID: "user-a"}, bytes.NewReader(validPNG(t)))
	if err == nil {
		t.Fatal("Upload() aceitou falha do Firestore")
	}
	path := "avatars/user-a/nova-foto.jpg"
	if len(store.deleted) != 1 || store.deleted[0] != path {
		t.Fatalf("compensação = %#v", store.deleted)
	}
	if _, exists := store.values[path]; exists {
		t.Fatal("novo objeto permaneceu após falha do Firestore")
	}
}

func TestUploadStorageFailureDoesNotChangeFirestore(t *testing.T) {
	repository := &serviceRepository{media: map[string]Media{}}
	store := &serviceStore{values: map[string][]byte{}, writeErr: errors.New("storage indisponível")}
	service := NewService(repository, store)
	service.newID = func() (string, error) { return "nova-foto", nil }
	if _, err := service.Upload(context.Background(), auth.Identity{UID: "user-a"}, bytes.NewReader(validPNG(t))); err == nil {
		t.Fatal("Upload() aceitou falha do Storage")
	}
	if repository.replaceCall != 0 {
		t.Fatalf("Firestore alterado %d vez(es)", repository.replaceCall)
	}
}

func TestReadRequiresOwnerAndInternalAvatarStartsCleanup(t *testing.T) {
	path := "avatars/user-a/foto.jpg"
	repository := &serviceRepository{media: map[string]Media{"foto": {ID: "foto", OwnerUID: "user-a", ObjectPath: path}}, oldPath: path}
	store := &serviceStore{values: map[string][]byte{path: []byte("jpeg")}}
	service := NewService(repository, store)
	if _, err := service.Read(context.Background(), auth.Identity{UID: "user-b"}, "foto"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outro usuário leu foto: %v", err)
	}
	if _, err := service.Read(context.Background(), auth.Identity{}, "foto"); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("identidade vazia: %v", err)
	}
	if value, err := service.Read(context.Background(), auth.Identity{UID: "user-a"}, "current"); err != nil || string(value) != "jpeg" {
		t.Fatalf("foto atual = %q, erro=%v", value, err)
	}
	if err := service.SelectInternal(context.Background(), auth.Identity{UID: "user-a"}, profile.AvatarGreen); err != nil {
		t.Fatal(err)
	}
	if repository.removeType != profile.AvatarGreen || len(store.deleted) != 1 || len(repository.cleaned) != 1 {
		t.Fatalf("seleção interna não limpou foto: tipo=%q deletes=%#v concluídos=%#v", repository.removeType, store.deleted, repository.cleaned)
	}
}

func TestReadStoredObjectRejectsExcessInsteadOfTruncating(t *testing.T) {
	exact := bytes.Repeat([]byte{'a'}, MaxStoredBytes)
	value, err := readStoredObject(bytes.NewReader(exact))
	if err != nil || len(value) != MaxStoredBytes {
		t.Fatalf("objeto no limite: bytes=%d erro=%v", len(value), err)
	}
	tooLarge := bytes.Repeat([]byte{'b'}, MaxStoredBytes+1)
	if value, err := readStoredObject(bytes.NewReader(tooLarge)); !errors.Is(err, ErrTooLarge) || value != nil {
		t.Fatalf("objeto excessivo: bytes=%d erro=%v", len(value), err)
	}
}

func validPNG(t *testing.T) []byte {
	t.Helper()
	var value bytes.Buffer
	if err := png.Encode(&value, image.NewRGBA(image.Rect(0, 0, 32, 24))); err != nil {
		t.Fatal(err)
	}
	return value.Bytes()
}

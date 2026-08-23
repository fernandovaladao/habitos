package avatar

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidImage = errors.New("imagem inválida")
	ErrTooLarge     = errors.New("imagem excede o limite permitido")
	ErrNotFound     = errors.New("foto não encontrada")
)

const (
	MaxUploadBytes = 5 * 1024 * 1024
	MaxPixels      = 20_000_000
	MaxSide        = 8192
	OutputSize     = 512
	JPEGQuality    = 85
)

type Media struct {
	ID         string    `firestore:"id"`
	OwnerUID   string    `firestore:"ownerUid"`
	ObjectPath string    `firestore:"objectPath"`
	CreatedAt  time.Time `firestore:"createdAt"`
}

type Repository interface {
	ReplacePhoto(ctx context.Context, uid string, media Media, now time.Time) (string, error)
	RemovePhoto(ctx context.Context, uid string, internalType string, now time.Time) (string, error)
	Media(ctx context.Context, mediaID string) (Media, error)
	CurrentMedia(ctx context.Context, uid string) (Media, error)
	CleanupCompleted(ctx context.Context, objectPath string) error
}

type ObjectStore interface {
	Write(ctx context.Context, path string, content []byte) error
	Read(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
}

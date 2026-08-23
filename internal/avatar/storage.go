package avatar

import (
	"context"
	"errors"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

type Storage struct{ bucket *storage.BucketHandle }

const MaxStoredBytes = 2 * 1024 * 1024

func NewStorage(bucket *storage.BucketHandle) *Storage { return &Storage{bucket: bucket} }

func (s *Storage) Write(ctx context.Context, path string, content []byte) error {
	writer := s.bucket.Object(path).NewWriter(ctx)
	writer.ContentType = "image/jpeg"
	writer.CacheControl = "private, max-age=0, must-revalidate"
	if _, err := writer.Write(content); err != nil {
		_ = writer.Close()
		return fmt.Errorf("gravar foto: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("concluir foto: %w", err)
	}
	return nil
}

func (s *Storage) Read(ctx context.Context, path string) ([]byte, error) {
	reader, err := s.bucket.Object(path).NewReader(ctx)
	if err != nil {
		return nil, ErrNotFound
	}
	defer reader.Close()
	value, err := readStoredObject(reader)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func readStoredObject(reader io.Reader) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, MaxStoredBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ler foto: %w", err)
	}
	if len(value) > MaxStoredBytes {
		return nil, ErrTooLarge
	}
	return value, nil
}

func (s *Storage) Delete(ctx context.Context, path string) error {
	err := s.bucket.Object(path).Delete(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == 404 {
		return nil
	}
	return err
}

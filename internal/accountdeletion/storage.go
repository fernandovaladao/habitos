package accountdeletion

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

type Storage struct{ bucket *storage.BucketHandle }

func NewStorage(bucket *storage.BucketHandle) *Storage { return &Storage{bucket: bucket} }

func (s *Storage) DeleteBatch(ctx context.Context, prefix string, limit int) (bool, error) {
	objects := s.bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	deleted := 0
	for deleted < limit {
		attrs, err := objects.Next()
		if err == iterator.Done {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("listar objetos da conta")
		}
		if err := s.bucket.Object(attrs.Name).Delete(ctx); err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
			return false, fmt.Errorf("excluir objeto da conta")
		}
		deleted++
	}
	return false, nil
}

func (s *Storage) Empty(ctx context.Context, prefix string) (bool, error) {
	_, err := s.bucket.Objects(ctx, &storage.Query{Prefix: prefix}).Next()
	if err == iterator.Done {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("verificar objetos da conta")
	}
	return false, nil
}

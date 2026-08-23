package ranking

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	firestorepb "cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}

func (r *FirestoreRepository) ordered() firestore.Query {
	return r.client.Collection(CollectionName).
		OrderBy("totalPoints", firestore.Desc).
		OrderBy("rankingReachedAt", firestore.Asc).
		OrderBy(firestore.DocumentID, firestore.Asc)
}

func (r *FirestoreRepository) Top(ctx context.Context, limit int) ([]Entry, error) {
	docs, err := r.ordered().Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("consultar Top %d: %w", limit, err)
	}
	result := make([]Entry, 0, len(docs))
	for _, doc := range docs {
		value, err := decodeEntry(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (r *FirestoreRepository) Get(ctx context.Context, uid string) (Entry, error) {
	doc, err := r.client.Collection(CollectionName).Doc(uid).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("buscar participante: %w", err)
	}
	return decodeEntry(doc)
}

func (r *FirestoreRepository) CountBefore(ctx context.Context, value Entry) (int, error) {
	query := r.before(value)
	result, err := query.NewAggregationQuery().WithCount("count").Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("calcular posição no ranking: %w", err)
	}
	count, ok := result["count"].(*firestorepb.Value)
	if !ok {
		return 0, errorsNewAggregationResult()
	}
	return int(count.GetIntegerValue()), nil
}

func (r *FirestoreRepository) Previous(ctx context.Context, value Entry) (*Entry, error) {
	docs, err := r.before(value).LimitToLast(1).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("buscar posição superior: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}
	previous, err := decodeEntry(docs[0])
	if err != nil {
		return nil, err
	}
	return &previous, nil
}

func (r *FirestoreRepository) before(value Entry) firestore.Query {
	ref := r.client.Collection(CollectionName).Doc(value.UID)
	return r.ordered().EndBefore(value.TotalPoints, value.RankingReachedAt, ref)
}

func decodeEntry(doc *firestore.DocumentSnapshot) (Entry, error) {
	var value Entry
	if err := doc.DataTo(&value); err != nil {
		return Entry{}, fmt.Errorf("decodificar participante: %w", err)
	}
	value.UID = doc.Ref.ID
	return value, nil
}

func errorsNewAggregationResult() error {
	return fmt.Errorf("resultado de contagem do ranking inválido")
}

func ReconcileTransaction(tx *firestore.Transaction, client *firestore.Client, input ProjectionInput) error {
	ref := client.Collection(CollectionName).Doc(input.UID)
	snapshot, err := tx.Get(ref)
	exists := err == nil
	if err != nil && status.Code(err) != codes.NotFound {
		return err
	}
	wanted, eligible := BuildProjection(input)
	if !eligible {
		if exists {
			return tx.Delete(ref)
		}
		return nil
	}
	if exists {
		current, err := decodeEntry(snapshot)
		if err == nil && sameProjection(current, wanted) && hasOnlyProjectionFields(snapshot.Data()) {
			return nil
		}
	}
	return tx.Set(ref, wanted)
}

func hasOnlyProjectionFields(data map[string]interface{}) bool {
	if len(data) != 6 {
		return false
	}
	for _, field := range []string{"nickname", "avatarType", "avatarUrl", "totalPoints", "rankingReachedAt", "updatedAt"} {
		if _, ok := data[field]; !ok {
			return false
		}
	}
	return true
}

func sameProjection(left, right Entry) bool {
	return left.UID == right.UID && left.Nickname == right.Nickname && left.AvatarType == right.AvatarType && left.AvatarURL == right.AvatarURL && left.TotalPoints == right.TotalPoints && left.RankingReachedAt.Equal(right.RankingReachedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

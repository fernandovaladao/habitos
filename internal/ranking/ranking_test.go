package ranking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"habitos/internal/auth"
)

func TestBuildProjectionUsesCreatedAtForUntouchedZero(t *testing.T) {
	created := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	entry, eligible := BuildProjection(ProjectionInput{UID: "u1", Nickname: "Luna", RankingOptIn: true, ProfileComplete: true, CreatedAt: created, UpdatedAt: created})
	if !eligible || !entry.RankingReachedAt.Equal(created) || entry.AvatarType != DefaultAvatarType || entry.AvatarURL != "" {
		t.Fatalf("projeção = %#v, elegível=%v", entry, eligible)
	}
	if _, eligible := BuildProjection(ProjectionInput{UID: "u1", Nickname: "Luna", RankingOptIn: false, ProfileComplete: true}); eligible {
		t.Fatal("opt-out não pode produzir projeção")
	}
	if _, eligible := BuildProjection(ProjectionInput{UID: "u1", Nickname: "", RankingOptIn: true, ProfileComplete: false}); eligible {
		t.Fatal("perfil incompleto não pode produzir projeção")
	}
}

func TestPublicEntrySerializationContainsOnlyAllowedPublicFields(t *testing.T) {
	encoded, err := json.Marshal(ToPublic(Entry{UID: "secret-uid", Nickname: "Luna", AvatarType: "default", TotalPoints: 100}, 2, true))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"secret-uid", "email", "age", "timezone", "streak", "rankingReachedAt", "updatedAt", "isSelf"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("resposta pública contém %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"position", "nickname", "avatar", "points"} {
		if !strings.Contains(text, required) {
			t.Fatalf("resposta pública não contém %q: %s", required, text)
		}
	}
}

type memoryRepository struct {
	top      []Entry
	self     Entry
	count    int
	previous *Entry
}

func (r *memoryRepository) Top(context.Context, int) ([]Entry, error) { return r.top, nil }
func (r *memoryRepository) Get(_ context.Context, uid string) (Entry, error) {
	if r.self.UID != uid {
		return Entry{}, ErrNotFound
	}
	return r.self, nil
}
func (r *memoryRepository) CountBefore(context.Context, Entry) (int, error) { return r.count, nil }
func (r *memoryRepository) Previous(context.Context, Entry) (*Entry, error) { return r.previous, nil }

func TestBoardReturnsTopPositionAndPointsNeededToSurpass(t *testing.T) {
	previous := Entry{UID: "u10", Nickname: "Theo", TotalPoints: 100}
	repository := &memoryRepository{
		top:  []Entry{{UID: "u1", Nickname: "Luna", TotalPoints: 300}, previous},
		self: Entry{UID: "self", Nickname: "Nico", TotalPoints: 100}, count: 46, previous: &previous,
	}
	board, err := NewService(repository).Board(context.Background(), auth.Identity{UID: "self"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Top) != 2 || board.Self == nil || board.Self.Position != 47 || board.PointsToSurpass == nil || *board.PointsToSurpass != 1 {
		t.Fatalf("quadro = %#v", board)
	}
}

func TestBoardForOptOutReadsOnlyTop(t *testing.T) {
	repository := &memoryRepository{top: []Entry{{UID: "u1", Nickname: "Luna", TotalPoints: 300}}}
	board, err := NewService(repository).Board(context.Background(), auth.Identity{UID: "viewer"}, false)
	if err != nil || board.Participating || board.Self != nil || len(board.Top) != 1 {
		t.Fatalf("quadro = %#v, erro=%v", board, err)
	}
	if _, err := NewService(repository).Board(context.Background(), auth.Identity{}, false); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("identidade vazia: %v", err)
	}
}

func TestFirstPlaceHasNoDistance(t *testing.T) {
	repository := &memoryRepository{self: Entry{UID: "self", Nickname: "Luna", TotalPoints: 300}}
	board, err := NewService(repository).Board(context.Background(), auth.Identity{UID: "self"}, true)
	if err != nil || board.Self == nil || board.Self.Position != 1 || board.PointsToSurpass != nil {
		t.Fatalf("quadro = %#v, erro=%v", board, err)
	}
}

func TestPositionReadsOnlyAuthenticatedParticipant(t *testing.T) {
	repository := &memoryRepository{self: Entry{UID: "self", Nickname: "Luna", AvatarType: "purple", TotalPoints: 300}, count: 4}
	position, err := NewService(repository).Position(context.Background(), auth.Identity{UID: "self"})
	if err != nil || position.Position != 5 || position.Avatar.Type != "purple" {
		t.Fatalf("posição=%#v erro=%v", position, err)
	}
	if _, err := NewService(repository).Position(context.Background(), auth.Identity{}); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("identidade vazia: %v", err)
	}
}

package ranking

import (
	"context"

	"habitos/internal/auth"
)

type Repository interface {
	Top(ctx context.Context, limit int) ([]Entry, error)
	Get(ctx context.Context, uid string) (Entry, error)
	CountBefore(ctx context.Context, value Entry) (int, error)
	Previous(ctx context.Context, value Entry) (*Entry, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Board(ctx context.Context, identity auth.Identity, participating bool) (Board, error) {
	if identity.UID == "" {
		return Board{}, auth.ErrInvalidSession
	}
	top, err := s.repository.Top(ctx, TopLimit)
	if err != nil {
		return Board{}, err
	}
	board := Board{Participating: participating, Top: make([]PublicEntry, len(top))}
	for index, item := range top {
		board.Top[index] = ToPublic(item, index+1, participating && item.UID == identity.UID)
	}
	if !participating {
		return board, nil
	}
	self, err := s.repository.Get(ctx, identity.UID)
	if err != nil {
		return Board{}, err
	}
	before, err := s.repository.CountBefore(ctx, self)
	if err != nil {
		return Board{}, err
	}
	publicSelf := ToPublic(self, before+1, true)
	board.Self = &publicSelf
	if before > 0 {
		previous, err := s.repository.Previous(ctx, self)
		if err != nil {
			return Board{}, err
		}
		if previous != nil {
			distance := PointsNeededToSurpass(self.TotalPoints, previous.TotalPoints)
			board.PointsToSurpass = &distance
		}
	}
	return board, nil
}

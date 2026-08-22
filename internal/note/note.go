package note

import (
	"context"
	"errors"
	"habitos/internal/auth"
	"habitos/internal/execution"
	"habitos/internal/habit"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound       = errors.New("nota não encontrada")
	ErrInvalidContent = errors.New("nota deve ter entre 1 e 1.000 caracteres")
)

type Note struct {
	ID          string    `firestore:"id" json:"id"`
	OwnerUID    string    `firestore:"ownerUid" json:"-"`
	HabitID     string    `firestore:"habitId" json:"habitId"`
	ExecutionID string    `firestore:"executionId,omitempty" json:"executionId,omitempty"`
	Content     string    `firestore:"content" json:"content"`
	CreatedAt   time.Time `firestore:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `firestore:"updatedAt" json:"updatedAt"`
}
type Repository interface {
	NewID() string
	Create(context.Context, Note) (Note, error)
	Get(context.Context, string, string) (Note, error)
	ListByHabit(context.Context, string, string) ([]Note, error)
	Update(context.Context, string, string, string, time.Time) (Note, error)
	Delete(context.Context, string, string) error
}
type HabitReader interface {
	Get(context.Context, auth.Identity, string) (habit.Habit, error)
}
type ExecutionReader interface {
	Get(context.Context, auth.Identity, string) (execution.Execution, error)
}
type Service struct {
	repository Repository
	habits     HabitReader
	executions ExecutionReader
	now        func() time.Time
}

func NewService(repository Repository, habits HabitReader, executions ExecutionReader) *Service {
	return &Service{repository: repository, habits: habits, executions: executions, now: time.Now}
}
func (s *Service) Create(ctx context.Context, identity auth.Identity, habitID, executionID, content string) (Note, error) {
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) < 1 || utf8.RuneCountInString(content) > 1000 {
		return Note{}, ErrInvalidContent
	}
	if _, err := s.habits.Get(ctx, identity, habitID); err != nil {
		return Note{}, err
	}
	if executionID != "" {
		value, err := s.executions.Get(ctx, identity, executionID)
		if err != nil {
			return Note{}, err
		}
		if value.HabitID != habitID {
			return Note{}, execution.ErrNotFound
		}
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	return s.repository.Create(ctx, Note{ID: s.repository.NewID(), OwnerUID: identity.UID, HabitID: habitID, ExecutionID: executionID, Content: content, CreatedAt: now, UpdatedAt: now})
}
func (s *Service) List(ctx context.Context, identity auth.Identity, habitID string) ([]Note, error) {
	if _, err := s.habits.Get(ctx, identity, habitID); err != nil {
		return nil, err
	}
	return s.repository.ListByHabit(ctx, identity.UID, habitID)
}
func (s *Service) Update(ctx context.Context, identity auth.Identity, id, content string) (Note, error) {
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) < 1 || utf8.RuneCountInString(content) > 1000 {
		return Note{}, ErrInvalidContent
	}
	return s.repository.Update(ctx, identity.UID, id, content, s.now().UTC().Truncate(time.Microsecond))
}
func (s *Service) Delete(ctx context.Context, identity auth.Identity, id string) error {
	return s.repository.Delete(ctx, identity.UID, id)
}

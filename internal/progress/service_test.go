package progress

import (
	"context"
	"errors"
	"testing"
	"time"

	"habitos/internal/auth"
	"habitos/internal/execution"
	"habitos/internal/gamification"
)

type stubRepository struct {
	executions []execution.Execution
	queries    [][3]string
}

func (r *stubRepository) Executions(_ context.Context, uid, start, end string) ([]execution.Execution, error) {
	r.queries = append(r.queries, [3]string{uid, start, end})
	return r.executions, nil
}
func (*stubRepository) BonusAwards(context.Context, string, string, string) ([]gamification.BonusAward, error) {
	return nil, nil
}
func (*stubRepository) Streaks(context.Context, string) ([]gamification.Streak, error) {
	return nil, nil
}
func (*stubRepository) Achievements(context.Context, string) ([]gamification.UserAchievement, error) {
	return nil, nil
}
func (*stubRepository) Habits(context.Context, string, []string) (map[string]HabitDescriptor, error) {
	return map[string]HabitDescriptor{}, nil
}

func TestServiceRequiresAuthenticatedUIDAndQueriesEffectiveDates(t *testing.T) {
	repository := &stubRepository{}
	service := NewService(repository)
	service.now = func() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) }
	if _, err := service.Report(context.Background(), auth.Identity{}, "UTC", Query{}); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("erro = %v", err)
	}
	if _, err := service.Report(context.Background(), auth.Identity{UID: "owner"}, "UTC", Query{Kind: PeriodWeek}); err != nil {
		t.Fatal(err)
	}
	if len(repository.queries) != 1 || repository.queries[0] != [3]string{"owner", "2026-08-17", "2026-08-22"} {
		t.Fatalf("consulta = %v", repository.queries)
	}
}

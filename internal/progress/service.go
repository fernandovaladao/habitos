package progress

import (
	"context"
	"time"

	"habitos/internal/auth"
	"habitos/internal/execution"
	"habitos/internal/gamification"
)

type Repository interface {
	Executions(ctx context.Context, ownerUID, startDate, endDate string) ([]execution.Execution, error)
	BonusAwards(ctx context.Context, ownerUID, startDate, endDate string) ([]gamification.BonusAward, error)
	Streaks(ctx context.Context, ownerUID string) ([]gamification.Streak, error)
	Achievements(ctx context.Context, ownerUID string) ([]gamification.UserAchievement, error)
	Habits(ctx context.Context, ownerUID string, habitIDs []string) (map[string]HabitDescriptor, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Report(ctx context.Context, identity auth.Identity, timezone string, query Query) (Report, error) {
	if identity.UID == "" {
		return Report{}, auth.ErrInvalidSession
	}
	period, err := ResolvePeriod(query, s.now(), timezone)
	if err != nil {
		return Report{}, err
	}
	executions, err := s.repository.Executions(ctx, identity.UID, period.StartDate, period.EffectiveEnd)
	if err != nil {
		return Report{}, err
	}
	bonuses, err := s.repository.BonusAwards(ctx, identity.UID, period.StartDate, period.EffectiveEnd)
	if err != nil {
		return Report{}, err
	}
	streaks, err := s.repository.Streaks(ctx, identity.UID)
	if err != nil {
		return Report{}, err
	}
	achievements, err := s.repository.Achievements(ctx, identity.UID)
	if err != nil {
		return Report{}, err
	}
	ids := uniqueHabitIDs(executions, streaks)
	habits, err := s.repository.Habits(ctx, identity.UID, ids)
	if err != nil {
		return Report{}, err
	}
	return Calculate(period, executions, bonuses, streaks, achievements, habits)
}

// WeekSummary performs one executions query for the user's current civil week
// and groups the exact rates in memory.
func (s *Service) WeekSummary(ctx context.Context, identity auth.Identity, timezone string) (WeeklySummary, error) {
	if identity.UID == "" {
		return WeeklySummary{}, auth.ErrInvalidSession
	}
	period, err := ResolvePeriod(Query{Kind: PeriodWeek}, s.now(), timezone)
	if err != nil {
		return WeeklySummary{}, err
	}
	executions, err := s.repository.Executions(ctx, identity.UID, period.StartDate, period.EffectiveEnd)
	if err != nil {
		return WeeklySummary{}, err
	}
	return CalculateWeeklySummary(period, executions)
}

func uniqueHabitIDs(executions []execution.Execution, streaks []gamification.Streak) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range executions {
		if !seen[item.HabitID] {
			seen[item.HabitID] = true
			result = append(result, item.HabitID)
		}
	}
	for _, item := range streaks {
		if !seen[item.HabitID] {
			seen[item.HabitID] = true
			result = append(result, item.HabitID)
		}
	}
	return result
}

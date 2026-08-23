package progress

import (
	"errors"
	"math/big"
	"time"

	"habitos/internal/gamification"
	"habitos/internal/habit"
)

var (
	ErrInvalidPeriod = errors.New("período de progresso inválido")
	ErrPeriodTooLong = errors.New("o período personalizado deve ter no máximo 366 dias")
	ErrInvalidData   = errors.New("dados de execução inválidos para progresso")
)

type PeriodKind string

const (
	PeriodWeek   PeriodKind = "week"
	PeriodMonth  PeriodKind = "month"
	PeriodCustom PeriodKind = "custom"
)

type Period struct {
	Kind         PeriodKind
	StartDate    string
	EndDate      string
	EffectiveEnd string
}

type Rate struct {
	Contribution *big.Rat
	Denominator  int
}

func EmptyRate() Rate { return Rate{Contribution: new(big.Rat)} }

type Counts struct {
	Completed int
	Partial   int
	NotDone   int
}

func (c Counts) Resolved() int { return c.Completed + c.Partial + c.NotDone }

type EvolutionPoint struct {
	StartDate string
	EndDate   string
	Rate      Rate
	Counts    Counts
	Points    int64
	Future    bool
}

type HabitProgress struct {
	HabitID string
	Title   string
	Status  habit.Status
	Deleted bool
	Rate    Rate
	Counts  Counts
	Points  int64
}

type Report struct {
	Period           Period
	Rate             Rate
	Counts           Counts
	Points           int64
	MaxCurrentStreak int
	Achievements     []gamification.UserAchievement
	Evolution        []EvolutionPoint
	ByHabit          []HabitProgress
}

type Query struct {
	Kind      PeriodKind
	StartDate string
	EndDate   string
}

func ResolvePeriod(query Query, now time.Time, timezone string) (Period, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Period{}, ErrInvalidPeriod
	}
	today := now.In(location)
	todayDate := today.Format("2006-01-02")
	kind := query.Kind
	if kind == "" {
		kind = PeriodWeek
	}
	var start, end time.Time
	switch kind {
	case PeriodWeek:
		weekday := int(today.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start = civilDate(today, -weekday+1, location)
		end = start.AddDate(0, 0, 6)
	case PeriodMonth:
		start = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
		end = start.AddDate(0, 1, -1)
	case PeriodCustom:
		if query.StartDate == "" && query.EndDate == "" {
			start = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
			end = civilDate(today, 0, location)
			break
		}
		start, err = time.ParseInLocation("2006-01-02", query.StartDate, location)
		if err != nil {
			return Period{}, ErrInvalidPeriod
		}
		end, err = time.ParseInLocation("2006-01-02", query.EndDate, location)
		if err != nil || start.After(end) {
			return Period{}, ErrInvalidPeriod
		}
		if inclusiveDays(start, end) > 366 {
			return Period{}, ErrPeriodTooLong
		}
	default:
		return Period{}, ErrInvalidPeriod
	}
	effectiveEnd := end.Format("2006-01-02")
	if effectiveEnd > todayDate {
		effectiveEnd = todayDate
	}
	if start.Format("2006-01-02") > effectiveEnd {
		return Period{}, ErrInvalidPeriod
	}
	return Period{Kind: kind, StartDate: start.Format("2006-01-02"), EndDate: end.Format("2006-01-02"), EffectiveEnd: effectiveEnd}, nil
}

func civilDate(value time.Time, addDays int, location *time.Location) time.Time {
	local := value.In(location).AddDate(0, 0, addDays)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func inclusiveDays(start, end time.Time) int {
	count := 1
	for date := start; date.Before(end); date = date.AddDate(0, 0, 1) {
		count++
	}
	return count
}

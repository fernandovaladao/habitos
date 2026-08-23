package progress

import (
	"math/big"
	"testing"

	"habitos/internal/execution"
	"habitos/internal/gamification"
	"habitos/internal/habit"
)

func TestCalculateUsesExactProportionalRateAndExcludesPending(t *testing.T) {
	period := Period{Kind: PeriodWeek, StartDate: "2026-08-17", EndDate: "2026-08-23", EffectiveEnd: "2026-08-22"}
	executions := []execution.Execution{
		{ID: "done", HabitID: "a", ScheduledDate: "2026-08-17", Status: execution.StatusCompleted, PointsAwarded: 10},
		{ID: "partial", HabitID: "a", ScheduledDate: "2026-08-18", Status: execution.StatusPartial, TargetHundredthsSnapshot: 300, AchievedHundredths: 100, PointsAwarded: 3},
		{ID: "miss", HabitID: "b", ScheduledDate: "2026-08-19", Status: execution.StatusNotDone},
		{ID: "open", HabitID: "a", ScheduledDate: "2026-08-20", Status: execution.StatusPending},
	}
	report, err := Calculate(period, executions, []gamification.BonusAward{{HabitID: "a", Points: 10, TriggerScheduledDate: "2026-08-17"}}, nil, nil, map[string]HabitDescriptor{
		"a": {ID: "a", Title: "Água", Status: habit.StatusActive},
		"b": {ID: "b", Title: "Correr", Status: habit.StatusActive},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Rate.Denominator != 3 || report.Rate.Contribution.Cmp(big.NewRat(4, 3)) != 0 {
		t.Fatalf("taxa exata = %s / %d", report.Rate.Contribution, report.Rate.Denominator)
	}
	if report.Counts != (Counts{Completed: 1, Partial: 1, NotDone: 1}) || report.Points != 23 {
		t.Fatalf("agregados = %+v, pontos=%d", report.Counts, report.Points)
	}
	if len(report.ByHabit) != 2 || len(report.Evolution) != 7 || !report.Evolution[6].Future {
		t.Fatalf("detalhamento/evolução inesperados: hábitos=%d evolução=%+v", len(report.ByHabit), report.Evolution)
	}
}

func TestCalculateWeeklySummaryGroupsOneExecutionSetByHabit(t *testing.T) {
	period := Period{Kind: PeriodWeek, StartDate: "2026-08-17", EndDate: "2026-08-23", EffectiveEnd: "2026-08-22"}
	summary, err := CalculateWeeklySummary(period, []execution.Execution{
		{HabitID: "a", ScheduledDate: "2026-08-17", Status: execution.StatusCompleted},
		{HabitID: "a", ScheduledDate: "2026-08-18", Status: execution.StatusPartial, TargetHundredthsSnapshot: 400, AchievedHundredths: 100},
		{HabitID: "b", ScheduledDate: "2026-08-19", Status: execution.StatusNotDone},
		{HabitID: "b", ScheduledDate: "2026-08-22", Status: execution.StatusPending},
		{HabitID: "outside", ScheduledDate: "2026-08-16", Status: execution.StatusCompleted},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Rate.Denominator != 3 || summary.Rate.Contribution.Cmp(big.NewRat(5, 4)) != 0 {
		t.Fatalf("taxa geral = %s/%d", summary.Rate.Contribution, summary.Rate.Denominator)
	}
	if rate := summary.ByHabit["a"]; rate.Denominator != 2 || rate.Contribution.Cmp(big.NewRat(5, 4)) != 0 {
		t.Fatalf("taxa do hábito a = %s/%d", rate.Contribution, rate.Denominator)
	}
	if rate := summary.ByHabit["b"]; rate.Denominator != 1 || rate.Contribution.Sign() != 0 {
		t.Fatalf("taxa do hábito b = %s/%d", rate.Contribution, rate.Denominator)
	}
	if summary.TodayByHabit["b"].Status != execution.StatusPending {
		t.Fatalf("execução de hoje = %+v", summary.TodayByHabit)
	}
}

func TestCalculateMasksDeletedHabitAndUsesOnlyActiveStreak(t *testing.T) {
	period := Period{Kind: PeriodMonth, StartDate: "2026-08-01", EndDate: "2026-08-31", EffectiveEnd: "2026-08-22"}
	report, err := Calculate(period,
		[]execution.Execution{{HabitID: "deleted", ScheduledDate: "2026-08-10", Status: execution.StatusCompleted}}, nil,
		[]gamification.Streak{{HabitID: "active", CurrentStreak: 4}, {HabitID: "archived", CurrentStreak: 9}, {HabitID: "deleted", CurrentStreak: 20}}, nil,
		map[string]HabitDescriptor{
			"active":   {ID: "active", Title: "Ativo", Status: habit.StatusActive},
			"archived": {ID: "archived", Title: "Arquivado", Status: habit.StatusArchived},
			"deleted":  {ID: "deleted", Title: "Título privado", Status: habit.StatusActive, Deleted: true},
		})
	if err != nil {
		t.Fatal(err)
	}
	if report.MaxCurrentStreak != 4 {
		t.Fatalf("maior sequência = %d", report.MaxCurrentStreak)
	}
	if len(report.ByHabit) != 1 || report.ByHabit[0].Title != "Hábito excluído" || !report.ByHabit[0].Deleted {
		t.Fatalf("hábito excluído = %+v", report.ByHabit)
	}
}

func TestCalculateOrdersByRateTitleAndID(t *testing.T) {
	period := Period{Kind: PeriodCustom, StartDate: "2026-01-01", EndDate: "2026-01-03", EffectiveEnd: "2026-01-03"}
	executions := []execution.Execution{
		{HabitID: "c", ScheduledDate: "2026-01-01", Status: execution.StatusPartial, TargetHundredthsSnapshot: 200, AchievedHundredths: 100},
		{HabitID: "b", ScheduledDate: "2026-01-02", Status: execution.StatusCompleted},
		{HabitID: "a", ScheduledDate: "2026-01-03", Status: execution.StatusCompleted},
	}
	report, err := Calculate(period, executions, nil, nil, nil, map[string]HabitDescriptor{
		"a": {ID: "a", Title: "Mesmo", Status: habit.StatusActive}, "b": {ID: "b", Title: "Mesmo", Status: habit.StatusActive}, "c": {ID: "c", Title: "Outro", Status: habit.StatusActive},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{report.ByHabit[0].HabitID, report.ByHabit[1].HabitID, report.ByHabit[2].HabitID}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("ordem = %v", got)
	}
}

func TestEvolutionGroupsPeriodsLongerThan31DaysByCivilWeek(t *testing.T) {
	period := Period{Kind: PeriodCustom, StartDate: "2026-07-29", EndDate: "2026-09-02", EffectiveEnd: "2026-09-02"}
	report, err := Calculate(period, []execution.Execution{
		{HabitID: "a", ScheduledDate: "2026-07-29", Status: execution.StatusCompleted},
		{HabitID: "a", ScheduledDate: "2026-08-03", Status: execution.StatusNotDone},
		{HabitID: "a", ScheduledDate: "2026-09-02", Status: execution.StatusCompleted},
	}, nil, nil, nil, map[string]HabitDescriptor{"a": {ID: "a", Title: "A", Status: habit.StatusActive}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Evolution) != 6 || report.Evolution[0].StartDate != "2026-07-29" || report.Evolution[0].EndDate != "2026-08-02" || report.Evolution[5].StartDate != "2026-08-31" || report.Evolution[5].EndDate != "2026-09-02" {
		t.Fatalf("semanas civis = %+v", report.Evolution)
	}
}

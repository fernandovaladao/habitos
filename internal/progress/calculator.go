package progress

import (
	"math/big"
	"sort"
	"time"

	"habitos/internal/execution"
	"habitos/internal/gamification"
	"habitos/internal/habit"
)

type HabitDescriptor struct {
	ID      string
	Title   string
	Status  habit.Status
	Deleted bool
}

func Calculate(period Period, executions []execution.Execution, bonuses []gamification.BonusAward, streaks []gamification.Streak, achievements []gamification.UserAchievement, habits map[string]HabitDescriptor) (Report, error) {
	report := Report{Period: period, Rate: EmptyRate(), Achievements: achievements}
	byHabit := make(map[string]*HabitProgress)
	daily := make(map[string]*EvolutionPoint)
	for _, item := range executions {
		if item.ScheduledDate < period.StartDate || item.ScheduledDate > period.EffectiveEnd {
			continue
		}
		contribution, resolved, err := executionContribution(item)
		if err != nil {
			return Report{}, err
		}
		if !resolved {
			continue
		}
		addExecution(&report.Rate, &report.Counts, item.Status, contribution)
		report.Points += int64(item.PointsAwarded)
		point := daily[item.ScheduledDate]
		if point == nil {
			point = &EvolutionPoint{StartDate: item.ScheduledDate, EndDate: item.ScheduledDate, Rate: EmptyRate()}
			daily[item.ScheduledDate] = point
		}
		addExecution(&point.Rate, &point.Counts, item.Status, contribution)
		point.Points += int64(item.PointsAwarded)
		descriptor := habits[item.HabitID]
		habitProgress := byHabit[item.HabitID]
		if habitProgress == nil {
			title := descriptor.Title
			if descriptor.Deleted || title == "" {
				title = "Hábito excluído"
			}
			habitProgress = &HabitProgress{HabitID: item.HabitID, Title: title, Status: descriptor.Status, Deleted: descriptor.Deleted || descriptor.ID == "", Rate: EmptyRate()}
			byHabit[item.HabitID] = habitProgress
		}
		addExecution(&habitProgress.Rate, &habitProgress.Counts, item.Status, contribution)
		habitProgress.Points += int64(item.PointsAwarded)
	}
	for _, bonus := range bonuses {
		if bonus.TriggerScheduledDate < period.StartDate || bonus.TriggerScheduledDate > period.EffectiveEnd {
			continue
		}
		report.Points += bonus.Points
		if point := daily[bonus.TriggerScheduledDate]; point != nil {
			point.Points += bonus.Points
		}
		if item := byHabit[bonus.HabitID]; item != nil {
			item.Points += bonus.Points
		}
	}
	for _, streak := range streaks {
		descriptor, ok := habits[streak.HabitID]
		if ok && !descriptor.Deleted && descriptor.Status == habit.StatusActive && streak.CurrentStreak > report.MaxCurrentStreak {
			report.MaxCurrentStreak = streak.CurrentStreak
		}
	}
	report.Evolution = buildEvolution(period, daily)
	for _, item := range byHabit {
		report.ByHabit = append(report.ByHabit, *item)
	}
	sort.Slice(report.ByHabit, func(i, j int) bool {
		comparison := CompareRate(report.ByHabit[i].Rate, report.ByHabit[j].Rate)
		if comparison != 0 {
			return comparison > 0
		}
		if report.ByHabit[i].Title != report.ByHabit[j].Title {
			return report.ByHabit[i].Title < report.ByHabit[j].Title
		}
		return report.ByHabit[i].HabitID < report.ByHabit[j].HabitID
	})
	return report, nil
}

func executionContribution(value execution.Execution) (*big.Rat, bool, error) {
	switch value.Status {
	case execution.StatusPending:
		return new(big.Rat), false, nil
	case execution.StatusCompleted:
		return big.NewRat(1, 1), true, nil
	case execution.StatusNotDone:
		return new(big.Rat), true, nil
	case execution.StatusPartial:
		if value.TargetHundredthsSnapshot <= 0 || value.AchievedHundredths <= 0 {
			return nil, false, ErrInvalidData
		}
		if value.AchievedHundredths >= value.TargetHundredthsSnapshot {
			return big.NewRat(1, 1), true, nil
		}
		return big.NewRat(value.AchievedHundredths, value.TargetHundredthsSnapshot), true, nil
	default:
		return nil, false, ErrInvalidData
	}
}

func addExecution(rate *Rate, counts *Counts, status execution.Status, contribution *big.Rat) {
	if rate.Contribution == nil {
		rate.Contribution = new(big.Rat)
	}
	rate.Contribution.Add(rate.Contribution, contribution)
	rate.Denominator++
	switch status {
	case execution.StatusCompleted:
		counts.Completed++
	case execution.StatusPartial:
		counts.Partial++
	case execution.StatusNotDone:
		counts.NotDone++
	}
}

func CompareRate(left, right Rate) int {
	if left.Denominator == 0 && right.Denominator == 0 {
		return 0
	}
	if left.Denominator == 0 {
		return -1
	}
	if right.Denominator == 0 {
		return 1
	}
	l := new(big.Rat).Quo(new(big.Rat).Set(left.Contribution), big.NewRat(int64(left.Denominator), 1))
	r := new(big.Rat).Quo(new(big.Rat).Set(right.Contribution), big.NewRat(int64(right.Denominator), 1))
	return l.Cmp(r)
}

func buildEvolution(period Period, daily map[string]*EvolutionPoint) []EvolutionPoint {
	start, _ := time.Parse("2006-01-02", period.StartDate)
	end, _ := time.Parse("2006-01-02", period.EffectiveEnd)
	nominalEnd, _ := time.Parse("2006-01-02", period.EndDate)
	length := inclusiveDays(start, nominalEnd)
	if length <= 31 {
		axisEnd := end
		if period.Kind == PeriodWeek {
			axisEnd = nominalEnd
		}
		var result []EvolutionPoint
		for date := start; !date.After(axisEnd); date = date.AddDate(0, 0, 1) {
			key := date.Format("2006-01-02")
			if point := daily[key]; point != nil {
				result = append(result, *point)
			} else {
				result = append(result, EvolutionPoint{StartDate: key, EndDate: key, Rate: EmptyRate(), Future: date.After(end)})
			}
		}
		return result
	}
	var result []EvolutionPoint
	for bucketStart := start; !bucketStart.After(end); {
		weekday := int(bucketStart.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		bucketEnd := bucketStart.AddDate(0, 0, 7-weekday)
		if bucketEnd.After(end) {
			bucketEnd = end
		}
		point := EvolutionPoint{StartDate: bucketStart.Format("2006-01-02"), EndDate: bucketEnd.Format("2006-01-02"), Rate: EmptyRate()}
		for date := bucketStart; !date.After(bucketEnd); date = date.AddDate(0, 0, 1) {
			if dailyPoint := daily[date.Format("2006-01-02")]; dailyPoint != nil {
				point.Rate.Contribution.Add(point.Rate.Contribution, dailyPoint.Rate.Contribution)
				point.Rate.Denominator += dailyPoint.Rate.Denominator
				point.Counts.Completed += dailyPoint.Counts.Completed
				point.Counts.Partial += dailyPoint.Counts.Partial
				point.Counts.NotDone += dailyPoint.Counts.NotDone
				point.Points += dailyPoint.Points
			}
		}
		result = append(result, point)
		bucketStart = bucketEnd.AddDate(0, 0, 1)
	}
	return result
}

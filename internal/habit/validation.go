package habit

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var clockPattern = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

func Normalize(input Input) Input {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.CustomUnit = strings.TrimSpace(input.CustomUnit)
	input.Schedule.Weekdays = append([]int(nil), input.Schedule.Weekdays...)
	sort.Ints(input.Schedule.Weekdays)
	return input
}

func Validate(input Input) error {
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 120 || input.Description == "" || utf8.RuneCountInString(input.Description) > 1000 {
		return ErrInvalidInput
	}
	if input.GoalType != GoalSimple && input.GoalType != GoalQuantitative {
		return ErrInvalidGoal
	}
	if input.GoalType == GoalQuantitative && input.TargetHundredths <= 0 {
		return ErrInvalidGoal
	}
	if input.GoalType == GoalSimple && input.TargetHundredths != 0 {
		return ErrInvalidGoal
	}
	validUnits := map[Unit]bool{UnitPages: true, UnitMinutes: true, UnitKilometers: true, UnitTimes: true, UnitLiters: true, UnitOther: true}
	if input.GoalType == GoalQuantitative {
		if !validUnits[input.Unit] || (input.Unit == UnitOther && (input.CustomUnit == "" || utf8.RuneCountInString(input.CustomUnit) > 40)) || (input.Unit != UnitOther && input.CustomUnit != "") {
			return ErrInvalidUnit
		}
	} else if input.Unit != "" || input.CustomUnit != "" {
		return ErrInvalidUnit
	}
	if len(input.Schedule.Weekdays) == 0 || len(input.Schedule.Weekdays) > 7 || !clockPattern.MatchString(input.Schedule.Time) {
		return ErrInvalidSchedule
	}
	seen := map[int]bool{}
	for _, day := range input.Schedule.Weekdays {
		if day < 1 || day > 7 || seen[day] {
			return ErrInvalidSchedule
		}
		seen[day] = true
	}
	if input.Schedule.WeeklyTargetExecutions <= 0 || input.Schedule.WeeklyTargetExecutions > len(input.Schedule.Weekdays) {
		return ErrInvalidWeekly
	}
	if input.Schedule.Reminder != ReminderNotification && input.Schedule.Reminder != ReminderEmail && input.Schedule.Reminder != ReminderBoth {
		return ErrInvalidReminder
	}
	return nil
}

func SameSchedule(a, b Schedule) bool {
	if a.Time != b.Time || a.WeeklyTargetExecutions != b.WeeklyTargetExecutions || a.Reminder != b.Reminder || len(a.Weekdays) != len(b.Weekdays) {
		return false
	}
	for i := range a.Weekdays {
		if a.Weekdays[i] != b.Weekdays[i] {
			return false
		}
	}
	return true
}

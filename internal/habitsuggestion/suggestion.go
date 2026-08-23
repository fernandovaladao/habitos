package habitsuggestion

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"habitos/internal/habit"
)

var (
	ErrInvalidRequest    = errors.New("dados para sugestão inválidos")
	ErrInvalidSuggestion = errors.New("sugestão de hábito inválida")
	ErrProvider          = errors.New("não foi possível gerar a sugestão")
)

var (
	amountPattern = regexp.MustCompile(`^\d+(?:[.,]\d{1,2})?$`)
	clockPattern  = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)
)

type Request struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ProviderRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ProviderSuggestion struct {
	Title                  string  `json:"title"`
	Description            string  `json:"description"`
	GoalType               string  `json:"goalType"`
	Target                 string  `json:"target"`
	Unit                   string  `json:"unit"`
	CustomUnit             string  `json:"customUnit"`
	Weekdays               []int   `json:"weekdays"`
	WeeklyTargetExecutions int     `json:"weeklyTargetExecutions"`
	Time                   *string `json:"time"`
}

type Suggestion struct {
	Title                  string         `json:"title"`
	Description            string         `json:"description"`
	GoalType               habit.GoalType `json:"goalType"`
	Target                 string         `json:"target"`
	Unit                   habit.Unit     `json:"unit"`
	CustomUnit             string         `json:"customUnit"`
	Weekdays               []int          `json:"weekdays"`
	WeeklyTargetExecutions int            `json:"weeklyTargetExecutions"`
	Time                   string         `json:"time,omitempty"`
}

func NormalizeRequest(value Request) Request {
	value.Title = strings.TrimSpace(value.Title)
	value.Description = strings.TrimSpace(value.Description)
	return value
}

func ValidateRequest(value Request) error {
	if value.Title == "" || utf8.RuneCountInString(value.Title) > 120 || value.Description == "" || utf8.RuneCountInString(value.Description) > 1000 {
		return ErrInvalidRequest
	}
	return nil
}

func ValidateProviderSuggestion(raw ProviderSuggestion) (Suggestion, error) {
	value := Suggestion{
		Title: strings.TrimSpace(raw.Title), Description: strings.TrimSpace(raw.Description), GoalType: habit.GoalType(raw.GoalType),
		Target: strings.ReplaceAll(strings.TrimSpace(raw.Target), ",", "."), Unit: habit.Unit(raw.Unit), CustomUnit: strings.TrimSpace(raw.CustomUnit),
		Weekdays: append([]int(nil), raw.Weekdays...), WeeklyTargetExecutions: raw.WeeklyTargetExecutions,
	}
	if raw.Time != nil {
		value.Time = strings.TrimSpace(*raw.Time)
	}
	sort.Ints(value.Weekdays)
	if value.Title == "" || utf8.RuneCountInString(value.Title) > 120 || value.Description == "" || utf8.RuneCountInString(value.Description) > 1000 {
		return Suggestion{}, ErrInvalidSuggestion
	}
	if value.GoalType != habit.GoalSimple && value.GoalType != habit.GoalQuantitative {
		return Suggestion{}, ErrInvalidSuggestion
	}
	validUnits := map[habit.Unit]bool{habit.UnitPages: true, habit.UnitMinutes: true, habit.UnitKilometers: true, habit.UnitTimes: true, habit.UnitLiters: true, habit.UnitOther: true}
	if value.GoalType == habit.GoalSimple {
		if value.Target != "" || value.Unit != "" || value.CustomUnit != "" {
			return Suggestion{}, ErrInvalidSuggestion
		}
	} else {
		if !validUnits[value.Unit] || parsePositiveHundredths(value.Target) <= 0 {
			return Suggestion{}, ErrInvalidSuggestion
		}
		if value.Unit == habit.UnitOther {
			if value.CustomUnit == "" || utf8.RuneCountInString(value.CustomUnit) > 40 {
				return Suggestion{}, ErrInvalidSuggestion
			}
		} else if value.CustomUnit != "" {
			return Suggestion{}, ErrInvalidSuggestion
		}
	}
	if len(value.Weekdays) == 0 || len(value.Weekdays) > 7 || value.WeeklyTargetExecutions <= 0 || value.WeeklyTargetExecutions > len(value.Weekdays) {
		return Suggestion{}, ErrInvalidSuggestion
	}
	seen := make(map[int]bool, len(value.Weekdays))
	for _, day := range value.Weekdays {
		if day < 1 || day > 7 || seen[day] {
			return Suggestion{}, ErrInvalidSuggestion
		}
		seen[day] = true
	}
	if value.Time != "" && !clockPattern.MatchString(value.Time) {
		return Suggestion{}, ErrInvalidSuggestion
	}
	return value, nil
}

func parsePositiveHundredths(value string) int64 {
	if !amountPattern.MatchString(value) {
		return 0
	}
	parts := strings.Split(strings.ReplaceAll(value, ",", "."), ".")
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > (1<<63-1)/100 {
		return 0
	}
	fraction := int64(0)
	if len(parts) == 2 {
		fractionText := parts[1]
		if len(fractionText) == 1 {
			fractionText += "0"
		}
		fraction, err = strconv.ParseInt(fractionText, 10, 64)
		if err != nil {
			return 0
		}
	}
	return whole*100 + fraction
}

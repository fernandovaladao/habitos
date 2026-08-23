package habitsuggestion

import (
	"errors"
	"testing"

	"habitos/internal/habit"
)

func TestValidateProviderSuggestion(t *testing.T) {
	timeValue := "19:00"
	value, err := ValidateProviderSuggestion(ProviderSuggestion{Title: "  Ler diariamente  ", Description: " Ler um pouco todos os dias. ", GoalType: "quantitative", Target: "12,5", Unit: "pages", Weekdays: []int{5, 1, 3}, WeeklyTargetExecutions: 3, Time: &timeValue})
	if err != nil {
		t.Fatal(err)
	}
	if value.Title != "Ler diariamente" || value.Target != "12.5" || value.Unit != habit.UnitPages || value.Time != "19:00" || value.Weekdays[0] != 1 || value.Weekdays[2] != 5 {
		t.Fatalf("sugestão normalizada = %#v", value)
	}
}

func TestValidateProviderSuggestionAllowsSimpleAndOmittedTime(t *testing.T) {
	value, err := ValidateProviderSuggestion(ProviderSuggestion{Title: "Respirar", Description: "Faça uma pausa curta.", GoalType: "simple", Weekdays: []int{1}, WeeklyTargetExecutions: 1})
	if err != nil || value.Time != "" {
		t.Fatalf("sugestão = %#v, erro=%v", value, err)
	}
}

func TestValidateProviderSuggestionRejectsInvalidFields(t *testing.T) {
	tests := []ProviderSuggestion{
		{Title: "", Description: "D", GoalType: "simple", Weekdays: []int{1}, WeeklyTargetExecutions: 1},
		{Title: "T", Description: "D", GoalType: "quantitative", Target: "0", Unit: "pages", Weekdays: []int{1}, WeeklyTargetExecutions: 1},
		{Title: "T", Description: "D", GoalType: "quantitative", Target: "1.001", Unit: "pages", Weekdays: []int{1}, WeeklyTargetExecutions: 1},
		{Title: "T", Description: "D", GoalType: "quantitative", Target: "1", Unit: "unknown", Weekdays: []int{1}, WeeklyTargetExecutions: 1},
		{Title: "T", Description: "D", GoalType: "quantitative", Target: "1", Unit: "other", Weekdays: []int{1}, WeeklyTargetExecutions: 1},
		{Title: "T", Description: "D", GoalType: "simple", Weekdays: []int{1, 1}, WeeklyTargetExecutions: 1},
		{Title: "T", Description: "D", GoalType: "simple", Weekdays: []int{1}, WeeklyTargetExecutions: 2},
	}
	invalidTime := "25:00"
	tests = append(tests, ProviderSuggestion{Title: "T", Description: "D", GoalType: "simple", Weekdays: []int{1}, WeeklyTargetExecutions: 1, Time: &invalidTime})
	for index, input := range tests {
		if _, err := ValidateProviderSuggestion(input); !errors.Is(err, ErrInvalidSuggestion) {
			t.Fatalf("caso %d: erro=%v", index, err)
		}
	}
}

func TestValidateRequestRequiresValidTitleAndDescription(t *testing.T) {
	request := NormalizeRequest(Request{Title: "  Ler  ", Description: "  Criar rotina  "})
	if err := ValidateRequest(request); err != nil || request.Title != "Ler" || request.Description != "Criar rotina" {
		t.Fatalf("requisição=%#v erro=%v", request, err)
	}
	if err := ValidateRequest(Request{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("erro=%v", err)
	}
}

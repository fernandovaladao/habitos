package main

import (
	"testing"

	"habitos/internal/config"
	"habitos/internal/habitsuggestion"
)

func TestSuggestionProviderForWebUsesOpenAI(t *testing.T) {
	provider := newSuggestionProvider(config.Config{OpenAIAPIKey: "test-key", OpenAIModel: "test-model"})
	if _, ok := provider.(*habitsuggestion.OpenAIProvider); !ok {
		t.Fatalf("provider web = %T", provider)
	}
}

func TestSuggestionProviderForReminderProcessorIsDisabled(t *testing.T) {
	provider := newSuggestionProvider(config.Config{ReminderProcessor: true})
	if _, ok := provider.(habitsuggestion.DisabledProvider); !ok {
		t.Fatalf("provider processador = %T", provider)
	}
}

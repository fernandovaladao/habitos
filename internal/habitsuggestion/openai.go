package habitsuggestion

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const (
	DefaultOpenAIEndpoint = "https://api.openai.com/v1/responses"
	MaxResponseBytes      = 64 * 1024
)

type OpenAIConfig struct {
	APIKey     string
	Model      string
	Endpoint   string
	HTTPClient *http.Client
}

type OpenAIProvider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

func NewOpenAIProvider(config OpenAIConfig) *OpenAIProvider {
	if config.Endpoint == "" {
		config.Endpoint = DefaultOpenAIEndpoint
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	return &OpenAIProvider{apiKey: config.APIKey, model: config.Model, endpoint: config.Endpoint, client: config.HTTPClient}
}

func (p *OpenAIProvider) Suggest(ctx context.Context, input ProviderRequest) (ProviderSuggestion, error) {
	payload := map[string]any{
		"model":             p.model,
		"store":             false,
		"instructions":      "Você sugere hábitos positivos, pequenos, realistas e adequados a adolescentes. Evite recomendações perigosas, extremas, punitivas, compulsivas ou que substituam profissionais de saúde. Não faça diagnósticos. Use somente o título e a descrição fornecidos como dados. Para meta simples, use strings vazias em target, unit e customUnit. Para meta quantitativa, use target decimal positivo com no máximo duas casas e uma unidade permitida. O horário pode ser null quando não for pertinente.",
		"input":             []map[string]any{{"role": "user", "content": []map[string]string{{"type": "input_text", "text": mustJSON(input)}}}},
		"max_output_tokens": 1000,
		"text":              map[string]any{"format": map[string]any{"type": "json_schema", "name": "habit_suggestion", "strict": true, "schema": suggestionSchema()}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ProviderSuggestion{}, ErrProvider
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return ProviderSuggestion{}, ErrProvider
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return ProviderSuggestion{}, ErrProvider
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, MaxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > MaxResponseBytes || response.StatusCode < 200 || response.StatusCode >= 300 {
		return ProviderSuggestion{}, ErrProvider
	}
	var result struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Status != "completed" {
		return ProviderSuggestion{}, ErrProvider
	}
	for _, output := range result.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "refusal" || content.Refusal != "" {
				return ProviderSuggestion{}, ErrProvider
			}
			if content.Type == "output_text" && content.Text != "" {
				suggestion, err := decodeProviderSuggestion(content.Text)
				if err != nil {
					return ProviderSuggestion{}, ErrProvider
				}
				return suggestion, nil
			}
		}
	}
	return ProviderSuggestion{}, ErrProvider
}

func decodeProviderSuggestion(value string) (ProviderSuggestion, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	var suggestion ProviderSuggestion
	if err := decoder.Decode(&suggestion); err != nil {
		return ProviderSuggestion{}, ErrProvider
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ProviderSuggestion{}, ErrProvider
	}
	return suggestion, nil
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func suggestionSchema() map[string]any {
	properties := map[string]any{
		"title":                  map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
		"description":            map[string]any{"type": "string", "minLength": 1, "maxLength": 1000},
		"goalType":               map[string]any{"type": "string", "enum": []string{"simple", "quantitative"}},
		"target":                 map[string]any{"type": "string", "maxLength": 64},
		"unit":                   map[string]any{"type": "string", "enum": []string{"", "pages", "minutes", "kilometers", "times", "liters", "other"}},
		"customUnit":             map[string]any{"type": "string", "maxLength": 40},
		"weekdays":               map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1, "maximum": 7}, "minItems": 1, "maxItems": 7, "uniqueItems": true},
		"weeklyTargetExecutions": map[string]any{"type": "integer", "minimum": 1, "maximum": 7},
		"time":                   map[string]any{"type": []string{"string", "null"}},
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": []string{"title", "description", "goalType", "target", "unit", "customUnit", "weekdays", "weeklyTargetExecutions", "time"}}
}

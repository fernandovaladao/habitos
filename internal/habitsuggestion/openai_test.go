package habitsuggestion

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testHTTPClient(handler roundTripFunc) *http.Client {
	return &http.Client{Transport: handler}
}

func TestOpenAIProviderUsesResponsesStructuredOutputsAndNoStorage(t *testing.T) {
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("requisição inesperada: método=%s autorização=%q", request.Method, request.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(request.Body)
		text := string(body)
		for _, required := range []string{`"model":"configured-model"`, `"store":false`, `"type":"json_schema"`, `"strict":true`, `\"title\":\"Ler\"`, `\"description\":\"Livros\"`} {
			if !strings.Contains(text, required) {
				t.Errorf("payload não contém %s: %s", required, text)
			}
		}
		for _, forbidden := range []string{"private-uid", "email", "weight", "gender", "timezone"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("payload contém dado proibido %q: %s", forbidden, text)
			}
		}
		suggestion, _ := json.Marshal(ProviderSuggestion{Title: "Ler dez páginas", Description: "Comece com pouco.", GoalType: "quantitative", Target: "10", Unit: "pages", Weekdays: []int{1, 3, 5}, WeeklyTargetExecutions: 3})
		response, _ := json.Marshal(map[string]any{
			"status": "completed",
			"output": []any{map[string]any{
				"type": "message",
				"content": []any{map[string]any{
					"type": "output_text",
					"text": string(suggestion),
				}},
			}},
		})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(response)))}, nil
	})
	provider := NewOpenAIProvider(OpenAIConfig{APIKey: "test-key", Model: "configured-model", Endpoint: "https://example.test/responses", HTTPClient: client})
	result, err := provider.Suggest(context.Background(), ProviderRequest{Title: "Ler", Description: "Livros"})
	if err != nil || result.Title != "Ler dez páginas" {
		t.Fatalf("resultado=%#v erro=%v", result, err)
	}
}

func TestOpenAIProviderRejectsProviderBodyAndOversizedResponseGenerically(t *testing.T) {
	responses := []struct {
		status int
		body   string
	}{
		{status: http.StatusBadGateway, body: "segredo interno"},
		{status: http.StatusOK, body: strings.Repeat("x", MaxResponseBytes+1)},
	}
	for _, response := range responses {
		client := testHTTPClient(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: response.status, Body: io.NopCloser(strings.NewReader(response.body))}, nil
		})
		provider := NewOpenAIProvider(OpenAIConfig{APIKey: "key", Model: "model", Endpoint: "https://example.test/responses", HTTPClient: client})
		_, err := provider.Suggest(context.Background(), ProviderRequest{Title: "T", Description: "D"})
		if err != ErrProvider || strings.Contains(err.Error(), "segredo") {
			t.Fatalf("erro=%v", err)
		}
	}
}

func TestDecodeProviderSuggestionRejectsUntrustedJSON(t *testing.T) {
	valid := `{"title":"Ler","description":"Leia.","goalType":"simple","target":"","unit":"","customUnit":"","weekdays":[1],"weeklyTargetExecutions":1,"time":null}`
	if suggestion, err := decodeProviderSuggestion(valid); err != nil || suggestion.Title != "Ler" {
		t.Fatalf("resposta válida: sugestão=%#v erro=%v", suggestion, err)
	}

	for name, value := range map[string]string{
		"campo desconhecido": strings.TrimSuffix(valid, "}") + `,"unexpected":"value"}`,
		"segundo objeto":     valid + ` {"title":"outro"}`,
		"conteúdo adicional": valid + ` inválido`,
		"json malformado":    `{"title":`,
		"resposta vazia":     ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProviderSuggestion(value); err != ErrProvider {
				t.Fatalf("erro=%v", err)
			}
		})
	}
}

package reminder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"time"
)

var ErrEmailProvider = errors.New("falha no provedor de e-mail")

type permanentProviderError struct{}

func (permanentProviderError) Error() string   { return ErrEmailProvider.Error() }
func (permanentProviderError) Permanent() bool { return true }

type ResendSender struct {
	apiKey, from, appURL string
	client               *http.Client
}

func NewResendSender(apiKey, from, appURL string, timeout time.Duration) *ResendSender {
	return &ResendSender{apiKey: apiKey, from: from, appURL: appURL, client: &http.Client{Timeout: timeout}}
}

func (s *ResendSender) SendReminder(ctx context.Context, message EmailMessage) error {
	payload := map[string]any{
		"from": s.from, "to": []string{message.To}, "subject": "Lembrete do HÁBITOS: " + message.HabitTitle,
		"html": fmt.Sprintf(`<p>Está na hora de <strong>%s</strong>, às %s.</p><p><a href="%s/meus-habitos?filtro=today">Abrir o HÁBITOS</a></p><p>Você pode alterar suas preferências no Perfil.</p>`, html.EscapeString(message.HabitTitle), html.EscapeString(message.ScheduledTime), html.EscapeString(s.appURL)),
	}
	body, _ := json.Marshal(payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return ErrEmailProvider
	}
	request.Header.Set("Authorization", "Bearer "+s.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", message.IdempotencyKey)
	response, err := s.client.Do(request)
	if err != nil {
		return ErrEmailProvider
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 4096)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusConflict && response.StatusCode != http.StatusTooManyRequests {
			return permanentProviderError{}
		}
		return ErrEmailProvider
	}
	return nil
}

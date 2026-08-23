package habitsuggestion

import (
	"context"
	"errors"
	"time"

	"habitos/internal/auth"
)

type Provider interface {
	Suggest(ctx context.Context, request ProviderRequest) (ProviderSuggestion, error)
}

type Service struct {
	provider Provider
	timeout  time.Duration
}

func NewService(provider Provider, timeout time.Duration) *Service {
	return &Service{provider: provider, timeout: timeout}
}

func (s *Service) Suggest(ctx context.Context, identity auth.Identity, request Request) (Suggestion, error) {
	if identity.UID == "" {
		return Suggestion{}, auth.ErrInvalidSession
	}
	request = NormalizeRequest(request)
	if err := ValidateRequest(request); err != nil {
		return Suggestion{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	raw, err := s.provider.Suggest(requestCtx, ProviderRequest{Title: request.Title, Description: request.Description})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return Suggestion{}, ErrProvider
		}
		return Suggestion{}, ErrProvider
	}
	return ValidateProviderSuggestion(raw)
}

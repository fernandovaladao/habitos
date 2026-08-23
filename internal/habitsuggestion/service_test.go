package habitsuggestion

import (
	"context"
	"errors"
	"testing"
	"time"

	"habitos/internal/auth"
)

type fakeProvider struct {
	result ProviderSuggestion
	err    error
	calls  int
	input  ProviderRequest
}

type blockingProvider struct{}

func (blockingProvider) Suggest(ctx context.Context, _ ProviderRequest) (ProviderSuggestion, error) {
	<-ctx.Done()
	return ProviderSuggestion{}, ctx.Err()
}

func (f *fakeProvider) Suggest(_ context.Context, input ProviderRequest) (ProviderSuggestion, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

func TestServiceSendsOnlyTitleAndDescriptionOnce(t *testing.T) {
	provider := &fakeProvider{result: ProviderSuggestion{Title: "Ler", Description: "Leia um pouco.", GoalType: "simple", Weekdays: []int{1}, WeeklyTargetExecutions: 1}}
	value, err := NewService(provider, time.Second).Suggest(context.Background(), auth.Identity{UID: "private-uid", Email: "private@example.test"}, Request{Title: " Ler ", Description: " Livros "})
	if err != nil || value.Title != "Ler" || provider.calls != 1 || provider.input != (ProviderRequest{Title: "Ler", Description: "Livros"}) {
		t.Fatalf("sugestão=%#v provedor=%#v erro=%v", value, provider, err)
	}
}

func TestServiceRejectsIdentityAndInvalidInputBeforeProvider(t *testing.T) {
	provider := &fakeProvider{}
	service := NewService(provider, time.Second)
	if _, err := service.Suggest(context.Background(), auth.Identity{}, Request{Title: "Ler", Description: "D"}); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("identidade: %v", err)
	}
	if _, err := service.Suggest(context.Background(), auth.Identity{UID: "u1"}, Request{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("entrada: %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provedor chamado %d vezes", provider.calls)
	}
}

func TestServiceReturnsGenericProviderErrorWithoutRetry(t *testing.T) {
	provider := &fakeProvider{err: errors.New("corpo secreto do provedor")}
	_, err := NewService(provider, time.Second).Suggest(context.Background(), auth.Identity{UID: "u1"}, Request{Title: "Ler", Description: "D"})
	if !errors.Is(err, ErrProvider) || provider.calls != 1 || errors.Is(err, provider.err) {
		t.Fatalf("erro=%v chamadas=%d", err, provider.calls)
	}
}

func TestServiceEnforcesRequestTimeout(t *testing.T) {
	started := time.Now()
	_, err := NewService(blockingProvider{}, time.Millisecond).Suggest(context.Background(), auth.Identity{UID: "u1"}, Request{Title: "Ler", Description: "D"})
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("erro=%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout não foi aplicado: %s", elapsed)
	}
}

func TestServiceRejectsExplicitDangerBeforeAndAfterProvider(t *testing.T) {
	provider := &fakeProvider{result: ProviderSuggestion{Title: "Ler", Description: "Leia um pouco.", GoalType: "simple", Weekdays: []int{1}, WeeklyTargetExecutions: 1}}
	service := NewService(provider, time.Second)
	if _, err := service.Suggest(context.Background(), auth.Identity{UID: "u1"}, Request{Title: "Meta", Description: "Ficar sem dormir"}); !errors.Is(err, ErrProvider) || provider.calls != 0 {
		t.Fatalf("entrada perigosa: erro=%v chamadas=%d", err, provider.calls)
	}
	provider.result.Description = "Treinar até desmaiar"
	if _, err := service.Suggest(context.Background(), auth.Identity{UID: "u1"}, Request{Title: "Treino", Description: "Criar rotina"}); !errors.Is(err, ErrProvider) || provider.calls != 1 {
		t.Fatalf("saída perigosa: erro=%v chamadas=%d", err, provider.calls)
	}
}

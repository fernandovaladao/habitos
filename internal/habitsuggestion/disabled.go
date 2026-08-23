package habitsuggestion

import "context"

// DisabledProvider mantém a dependência explícita no papel que não oferece
// sugestões, sem inicializar ou carregar credenciais de um provedor externo.
type DisabledProvider struct{}

func (DisabledProvider) Suggest(context.Context, ProviderRequest) (ProviderSuggestion, error) {
	return ProviderSuggestion{}, ErrProvider
}

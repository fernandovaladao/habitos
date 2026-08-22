package profile

import "testing"

func TestValidateUpdate(t *testing.T) {
	tests := []struct {
		name    string
		update  Update
		wantErr bool
	}{
		{name: "válido", update: Update{Nickname: "Ana_16", Age: 16, Timezone: "America/Sao_Paulo"}},
		{name: "apelido curto", update: Update{Nickname: "An", Age: 16, Timezone: "UTC"}, wantErr: true},
		{name: "caractere inválido", update: Update{Nickname: "Ana!", Age: 16, Timezone: "UTC"}, wantErr: true},
		{name: "idade zero", update: Update{Nickname: "Ana", Age: 0, Timezone: "UTC"}, wantErr: true},
		{name: "idade negativa", update: Update{Nickname: "Ana", Age: -1, Timezone: "UTC"}, wantErr: true},
		{name: "timezone inválido", update: Update{Nickname: "Ana", Age: 16, Timezone: "Sao_Paulo"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateUpdate(test.update)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateUpdate() erro = %v, wantErr = %v", err, test.wantErr)
			}
		})
	}
}

package habitsuggestion

import "testing"

func TestExplicitlyDangerousIsNarrowAndNormalizesText(t *testing.T) {
	for _, value := range []string{
		"Quero ficar sem comer por vários dias",
		"TREINAR, ATÉ DESMAIAR!",
		"Parar de tomar medicamento",
		"Provocar vômito depois das refeições",
		"Quero me machucar de propósito",
	} {
		if !explicitlyDangerous(value) {
			t.Errorf("conteúdo explicitamente perigoso não detectado")
		}
	}
	for _, value := range []string{
		"Tomar meu remédio no horário indicado",
		"Dormir oito horas",
		"Fazer exercício leve três vezes por semana",
		"Preparar uma refeição saudável",
	} {
		if explicitlyDangerous(value) {
			t.Errorf("conteúdo seguro foi bloqueado: %q", value)
		}
	}
}

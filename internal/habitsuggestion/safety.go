package habitsuggestion

import (
	"strings"
	"unicode"
)

// explicitlyDangerous is deliberately narrow. It is a best-effort guard for
// unambiguous harmful instructions, not a general medical-content classifier.
func explicitlyDangerous(values ...string) bool {
	text := normalizeSafetyText(strings.Join(values, " "))
	patterns := []string{
		" me cortar ", " cortar meu corpo ", " me machucar de proposito ",
		" tirar minha vida ", " cometer suicidio ",
		" ficar sem comer ", " parar de comer ", " passar o dia sem comer ", " passar dias sem comer ",
		" ficar sem beber agua ", " parar de beber agua ",
		" ficar sem dormir ", " passar a noite sem dormir ", " dormir zero horas ",
		" provocar vomito ", " induzir vomito ",
		" dobrar a dose do remedio ", " dobrar a dose do medicamento ",
		" parar de tomar remedio ", " parar de tomar medicamento ",
		" suspender meu remedio ", " suspender meu medicamento ",
		" treinar ate desmaiar ", " exercitar ate desmaiar ", " exercicio ate desmaiar ",
	}
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func normalizeSafetyText(value string) string {
	value = strings.ToLower(value)
	value = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u", "ç", "c",
	).Replace(value)
	var normalized strings.Builder
	normalized.WriteByte(' ')
	space := true
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			normalized.WriteRune(character)
			space = false
		} else if !space {
			normalized.WriteByte(' ')
			space = true
		}
	}
	if !space {
		normalized.WriteByte(' ')
	}
	return normalized.String()
}

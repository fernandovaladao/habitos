package profile

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidNickname = errors.New("apelido deve ter de 3 a 24 caracteres e usar apenas letras, números, espaços, _ ou -")
	ErrInvalidAge      = errors.New("idade deve ser um número inteiro positivo")
	ErrInvalidTimezone = errors.New("timezone deve usar um identificador IANA válido")
	nicknamePattern    = regexp.MustCompile(`^[\p{L}\p{N} _-]+$`)
)

func NormalizeNickname(value string) string {
	return strings.TrimSpace(value)
}

func ValidateNickname(value string) error {
	length := utf8.RuneCountInString(value)
	if length < 3 || length > 24 || !nicknamePattern.MatchString(value) {
		return ErrInvalidNickname
	}
	return nil
}

func ValidateTimezone(value string) error {
	if value == "" || value == "Local" {
		return ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(value); err != nil {
		return ErrInvalidTimezone
	}
	return nil
}

func ValidateUpdate(update Update) error {
	if err := ValidateNickname(update.Nickname); err != nil {
		return err
	}
	if update.Age <= 0 {
		return ErrInvalidAge
	}
	return ValidateTimezone(update.Timezone)
}

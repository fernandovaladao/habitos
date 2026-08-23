package progress

import (
	"errors"
	"testing"
	"time"
)

func TestResolvePeriodUsesUserCivilTimezone(t *testing.T) {
	now := time.Date(2026, 8, 3, 2, 30, 0, 0, time.UTC) // domingo em São Paulo
	week, err := ResolvePeriod(Query{Kind: PeriodWeek}, now, "America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	if week.StartDate != "2026-07-27" || week.EndDate != "2026-08-02" || week.EffectiveEnd != "2026-08-02" {
		t.Fatalf("semana = %+v", week)
	}
	month, err := ResolvePeriod(Query{Kind: PeriodMonth}, now, "America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	if month.StartDate != "2026-08-01" || month.EndDate != "2026-08-31" || month.EffectiveEnd != "2026-08-02" {
		t.Fatalf("mês = %+v", month)
	}
}

func TestResolveCustomPeriodInclusiveLimitAndDefaults(t *testing.T) {
	now := time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC)
	period, err := ResolvePeriod(Query{Kind: PeriodCustom, StartDate: "2024-01-01", EndDate: "2024-12-31"}, now, "UTC")
	if err != nil || period.StartDate != "2024-01-01" || period.EndDate != "2024-12-31" {
		t.Fatalf("366 dias deveriam ser aceitos: %+v, %v", period, err)
	}
	_, err = ResolvePeriod(Query{Kind: PeriodCustom, StartDate: "2023-01-01", EndDate: "2024-01-02"}, now, "UTC")
	if !errors.Is(err, ErrPeriodTooLong) {
		t.Fatalf("erro = %v, esperado ErrPeriodTooLong", err)
	}
	period, err = ResolvePeriod(Query{Kind: PeriodCustom}, now, "UTC")
	if err != nil || period.StartDate != "2026-08-01" || period.EndDate != "2026-08-22" {
		t.Fatalf("padrão do formulário = %+v, %v", period, err)
	}
}

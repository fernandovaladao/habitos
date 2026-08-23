package gamification

import (
	"math"
	"testing"
)

func TestQuantitativePoints(t *testing.T) {
	tests := []struct {
		name             string
		achieved, target int64
		want             int
	}{
		{"zero", 0, 10000, 0}, {"partial", 1200, 2000, 6}, {"half", 1000, 2000, 5},
		{"below half rounding", 649, 1000, 6}, {"exact half rounding", 650, 1000, 7}, {"above half rounding", 651, 1000, 7},
		{"equal", 2000, 2000, 10}, {"above", 2500, 2000, 10}, {"no overflow", math.MaxInt64 - 1, math.MaxInt64, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QuantitativePoints(tt.achieved, tt.target)
			if err != nil || got != tt.want {
				t.Fatalf("pontos=%d erro=%v; esperado %d", got, err, tt.want)
			}
		})
	}
}

func TestQuantitativePointsRejectsInvalidValues(t *testing.T) {
	for _, input := range [][2]int64{{-1, 100}, {1, 0}} {
		if _, err := QuantitativePoints(input[0], input[1]); err == nil {
			t.Fatalf("entrada aceita: %v", input)
		}
	}
}

func TestMilestonesReached(t *testing.T) {
	if got := MilestonesReached(2, 3); len(got) != 1 || got[0].Bonus != 10 {
		t.Fatalf("marco 3=%#v", got)
	}
	if got := MilestonesReached(30, 31); len(got) != 0 {
		t.Fatalf("sequência 31 gerou bônus: %#v", got)
	}
	for _, tt := range []struct{ before, after, milestone int }{{6, 7, 7}, {14, 15, 15}, {29, 30, 30}} {
		got := MilestonesReached(tt.before, tt.after)
		if len(got) != 1 || got[0].Value != tt.milestone {
			t.Fatalf("marco %d=%#v", tt.milestone, got)
		}
	}
}

func TestSequenceProjectionBreaksAndIgnoresUnscheduledDays(t *testing.T) {
	records := []SequenceRecord{{"2026-08-17", "completed"}, {"2026-08-19", "completed"}, {"2026-08-21", "completed"}}
	projection := ProjectSequence(records, 0, "")
	if projection.Current != 3 || projection.Best != 3 || projection.Steps[2].After != 3 {
		t.Fatalf("projeção=%#v", projection)
	}
	for _, status := range []string{"partial", "not_done"} {
		projection = ProjectSequence([]SequenceRecord{{"2026-08-17", "completed"}, {"2026-08-19", status}}, 0, "")
		if projection.Current != 0 {
			t.Fatalf("%s não quebrou sequência: %#v", status, projection)
		}
	}
}

func TestSequenceProjectionPendingGapAndRetroactiveCorrection(t *testing.T) {
	records := []SequenceRecord{{"2026-08-17", "completed"}, {"2026-08-18", "pending"}, {"2026-08-19", "completed"}}
	blocked := ProjectSequence(records, 4, "")
	if blocked.Current != 1 || blocked.Steps[2].Confirmed || blocked.Best != 4 {
		t.Fatalf("lacuna pendente=%#v", blocked)
	}
	records[1].Status = "completed"
	recalculated := ProjectSequence(records, blocked.Best, "")
	if recalculated.Current != 3 || recalculated.Best != 4 || !recalculated.Steps[2].Confirmed {
		t.Fatalf("recalculada=%#v", recalculated)
	}
	records[1].Status = "partial"
	reduced := ProjectSequence(records, recalculated.Best, "")
	if reduced.Current != 1 || reduced.Best != 4 {
		t.Fatalf("best histórico foi reduzido: %#v", reduced)
	}
}

func TestSequenceProjectionResetsOnReactivation(t *testing.T) {
	projection := ProjectSequence([]SequenceRecord{{"2026-08-17", "completed"}, {"2026-08-20", "completed"}}, 6, "2026-08-20")
	if projection.Current != 1 || projection.Best != 6 {
		t.Fatalf("reativação=%#v", projection)
	}
	withoutNewOccurrence := ProjectSequence([]SequenceRecord{{"2026-08-17", "completed"}}, 6, "2026-08-20")
	if withoutNewOccurrence.Current != 0 || withoutNewOccurrence.Best != 6 {
		t.Fatalf("reativação sem nova ocorrência=%#v", withoutNewOccurrence)
	}
}

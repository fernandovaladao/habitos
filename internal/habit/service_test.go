package habit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"habitos/internal/auth"
)

type memoryRepository struct {
	values   map[string]Habit
	versions map[string][]ScheduleVersion
	next     int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{values: map[string]Habit{}, versions: map[string][]ScheduleVersion{}}
}
func (r *memoryRepository) NewID() string { r.next++; return fmt.Sprintf("random-%d", r.next) }
func (r *memoryRepository) Create(_ context.Context, v Habit, s ScheduleVersion) error {
	r.values[v.ID] = v
	r.versions[v.ID] = append(r.versions[v.ID], s)
	return nil
}
func (r *memoryRepository) Get(_ context.Context, uid, id string) (Habit, error) {
	v, ok := r.values[id]
	if !ok || v.OwnerUID != uid || v.DeletedAt != nil {
		return Habit{}, ErrNotFound
	}
	return v, nil
}
func (r *memoryRepository) List(_ context.Context, uid string) ([]Habit, error) {
	var out []Habit
	for _, v := range r.values {
		if v.OwnerUID == uid && v.DeletedAt == nil {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *memoryRepository) ListScheduleVersions(_ context.Context, uid, id string) ([]ScheduleVersion, error) {
	if _, err := r.Get(context.Background(), uid, id); err != nil {
		return nil, err
	}
	return append([]ScheduleVersion(nil), r.versions[id]...), nil
}
func (r *memoryRepository) Update(_ context.Context, v Habit, s *ScheduleVersion) error {
	old, ok := r.values[v.ID]
	if !ok || old.OwnerUID != v.OwnerUID || old.DeletedAt != nil {
		return ErrNotFound
	}
	if s != nil {
		if old.PendingScheduleVersionID == s.ID && old.ScheduleEffectiveAt.Equal(s.EffectiveAt) {
			for index := range r.versions[v.ID] {
				if r.versions[v.ID][index].ID == s.ID {
					r.versions[v.ID][index] = *s
					r.values[v.ID] = v
					return nil
				}
			}
			return ErrInvalidTransition
		}
		r.versions[v.ID] = append(r.versions[v.ID], *s)
	}
	r.values[v.ID] = v
	return nil
}

func validInput() Input {
	return Input{Title: "Ler", Description: "Ler diariamente", GoalType: GoalQuantitative, TargetHundredths: 2000, Unit: UnitPages, Schedule: Schedule{Weekdays: []int{1, 3, 5}, Time: "19:00", WeeklyTargetExecutions: 3, Reminder: ReminderNotification}}
}
func serviceAt(repo Repository) *Service {
	s := NewService(repo)
	s.now = func() time.Time { return time.Date(2026, 8, 22, 15, 0, 0, 123456789, time.UTC) }
	return s
}

var userA = auth.Identity{UID: "a", Email: "a@example.test"}
var userB = auth.Identity{UID: "b", Email: "b@example.test"}

func TestCreateValidHabit(t *testing.T) {
	r := newMemoryRepository()
	h, err := serviceAt(r).Create(context.Background(), userA, "America/Sao_Paulo", validInput())
	if err != nil {
		t.Fatal(err)
	}
	if h.OwnerUID != "a" || h.ID == "" || len(r.versions[h.ID]) != 1 || h.CreatedAt.Nanosecond() != 123456000 || h.ScheduleEffectiveDate != "2026-08-22" || r.versions[h.ID][0].EffectiveDate != "2026-08-22" {
		t.Fatalf("hábito inesperado: %#v", h)
	}
}
func TestCreateSimpleHabit(t *testing.T) {
	r := newMemoryRepository()
	input := validInput()
	input.GoalType = GoalSimple
	input.TargetHundredths = 0
	input.Unit = ""
	habitValue, err := serviceAt(r).Create(context.Background(), userA, "UTC", input)
	if err != nil {
		t.Fatal(err)
	}
	if habitValue.GoalType != GoalSimple || habitValue.TargetHundredths != 0 {
		t.Fatalf("meta simples inválida: %#v", habitValue)
	}
}
func TestRejectsInvalidQuantitativeGoal(t *testing.T) {
	for _, value := range []int64{0, -1} {
		in := validInput()
		in.TargetHundredths = value
		_, err := serviceAt(newMemoryRepository()).Create(context.Background(), userA, "UTC", in)
		if !errors.Is(err, ErrInvalidGoal) {
			t.Fatalf("valor %d: %v", value, err)
		}
	}
}
func TestRejectsWeeklyTargetGreaterThanDays(t *testing.T) {
	in := validInput()
	in.Schedule.WeeklyTargetExecutions = 4
	_, err := serviceAt(newMemoryRepository()).Create(context.Background(), userA, "UTC", in)
	if !errors.Is(err, ErrInvalidWeekly) {
		t.Fatalf("erro=%v", err)
	}
}
func TestOwnerCannotAccessOrMutateAnotherUsersHabit(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "UTC", validInput())
	if _, err := s.Get(context.Background(), userB, h.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get erro=%v", err)
	}
	if _, err := s.Update(context.Background(), userB, "UTC", h.ID, validInput()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update erro=%v", err)
	}
	if err := s.Delete(context.Background(), userB, h.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete erro=%v", err)
	}
}
func TestArchiveAndReactivate(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "UTC", validInput())
	archived, err := s.Archive(context.Background(), userA, h.ID)
	if err != nil || archived.Status != StatusArchived || archived.ArchivedAt == nil {
		t.Fatalf("arquivar: %#v %v", archived, err)
	}
	active, err := s.Reactivate(context.Background(), userA, "America/Sao_Paulo", h.ID)
	if err != nil || active.Status != StatusActive || active.ArchivedAt != nil || active.ReactivatedAt == nil {
		t.Fatalf("reativar: %#v %v", active, err)
	}
	want := time.Date(2026, 8, 23, 3, 0, 0, 0, time.UTC)
	if !active.OccurrencesResumeAt.Equal(want) {
		t.Fatalf("retomada=%v, esperado %v", active.OccurrencesResumeAt, want)
	}
	if active.OccurrencesResumeDate != "2026-08-23" {
		t.Fatalf("data civil de retomada=%q", active.OccurrencesResumeDate)
	}
}
func TestDuplicateCreatesActiveHabitWithNewIdentityAndDates(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	source, _ := s.Create(context.Background(), userA, "UTC", validInput())
	source, _ = s.Archive(context.Background(), userA, source.ID)
	s.now = func() time.Time { return time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC) }
	copy, err := s.Duplicate(context.Background(), userA, "UTC", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if copy.ID == source.ID || copy.Status != StatusActive || copy.ArchivedAt != nil || copy.CreatedAt.Equal(source.CreatedAt) || !SameSchedule(copy.Schedule, source.Schedule) {
		t.Fatalf("cópia inválida: %#v", copy)
	}
}
func TestScheduleChangeStartsNextDayAndPreservesSnapshot(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "America/Sao_Paulo", validInput())
	input := validInput()
	input.Schedule.Time = "07:30"
	updated, err := s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 23, 3, 0, 0, 0, time.UTC)
	if !updated.ScheduleEffectiveAt.Equal(want) || updated.ScheduleEffectiveDate != "2026-08-23" || updated.PreviousSchedule == nil || len(r.versions[h.ID]) != 2 {
		t.Fatalf("agenda inválida: %#v versões=%d", updated, len(r.versions[h.ID]))
	}
	if updated.EffectiveSchedule(s.now()).Time != "19:00" {
		t.Fatal("agenda anterior não permaneceu vigente")
	}
}

func TestGoalChangeStartsNextDayAndPreservesCurrentSnapshot(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "America/Sao_Paulo", validInput())
	input := validInput()
	input.TargetHundredths = 3000
	updated, err := s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.versions[h.ID]) != 2 || r.versions[h.ID][0].TargetHundredths != 2000 || r.versions[h.ID][1].TargetHundredths != 3000 || r.versions[h.ID][1].EffectiveDate != "2026-08-23" {
		t.Fatalf("versões de meta inválidas: %#v", r.versions[h.ID])
	}
	if updated.TargetHundredths != 3000 {
		t.Fatal("projeção não contém meta futura")
	}
}

func TestSameDayScheduleEditsReplaceTheSinglePendingVersion(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "America/Sao_Paulo", validInput())
	historical := r.versions[h.ID][0]

	first := validInput()
	first.Schedule.Time = "07:30"
	firstEdit, err := s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	pendingID := firstEdit.PendingScheduleVersionID

	s.now = func() time.Time { return time.Date(2026, 8, 22, 18, 0, 0, 0, time.UTC) }
	last := validInput()
	last.Schedule.Time = "08:45"
	last.Schedule.Reminder = ReminderBoth
	lastEdit, err := s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, last)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.versions[h.ID]) != 2 {
		t.Fatalf("versões=%d; esperado snapshot histórico e uma versão pendente", len(r.versions[h.ID]))
	}
	if lastEdit.PendingScheduleVersionID != pendingID || r.versions[h.ID][1].ID != pendingID {
		t.Fatalf("versão pendente foi concorrente: primeira=%q última=%q", pendingID, lastEdit.PendingScheduleVersionID)
	}
	if r.versions[h.ID][1].Schedule.Time != "08:45" || r.versions[h.ID][1].Schedule.Reminder != ReminderBoth {
		t.Fatalf("versão pendente não contém a última escolha: %#v", r.versions[h.ID][1])
	}
	if !SameSchedule(r.versions[h.ID][0].Schedule, historical.Schedule) || !r.versions[h.ID][0].CreatedAt.Equal(historical.CreatedAt) {
		t.Fatal("versão histórica foi alterada")
	}

	today := time.Date(2026, 8, 22, 23, 59, 0, 0, time.FixedZone("-03", -3*60*60))
	tomorrow := time.Date(2026, 8, 23, 0, 0, 0, 0, time.FixedZone("-03", -3*60*60))
	if lastEdit.EffectiveSchedule(today).Time != "19:00" {
		t.Fatalf("hoje não usa agenda anterior: %#v", lastEdit.EffectiveSchedule(today))
	}
	if lastEdit.EffectiveSchedule(tomorrow).Time != "08:45" {
		t.Fatalf("amanhã não usa última edição: %#v", lastEdit.EffectiveSchedule(tomorrow))
	}
}

func TestEffectiveScheduleVersionIsNeverReplaced(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "America/Sao_Paulo", validInput())
	first := validInput()
	first.Schedule.Time = "07:30"
	firstEdit, _ := s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, first)
	firstPending := r.versions[h.ID][1]

	s.now = func() time.Time { return firstEdit.ScheduleEffectiveAt.Add(2 * time.Hour) }
	second := validInput()
	second.Schedule.Time = "09:15"
	secondEdit, err := s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.versions[h.ID]) != 3 || secondEdit.PendingScheduleVersionID == firstPending.ID {
		t.Fatalf("versão vigente foi reutilizada: versões=%d pending=%q", len(r.versions[h.ID]), secondEdit.PendingScheduleVersionID)
	}
	if !SameSchedule(r.versions[h.ID][1].Schedule, firstPending.Schedule) || !r.versions[h.ID][1].CreatedAt.Equal(firstPending.CreatedAt) {
		t.Fatal("versão que já entrou em vigor foi alterada")
	}
}

func TestDuplicateWithPendingScheduleUsesLatestChoiceAsInitialSchedule(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "America/Sao_Paulo", validInput())
	first := validInput()
	first.Schedule.Time = "07:30"
	_, _ = s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, first)
	last := validInput()
	last.Schedule.Time = "08:45"
	last.Schedule.Reminder = ReminderBoth
	_, _ = s.Update(context.Background(), userA, "America/Sao_Paulo", h.ID, last)

	duplicate, err := s.Duplicate(context.Background(), userA, "America/Sao_Paulo", h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Schedule.Time != "08:45" || duplicate.Schedule.Reminder != ReminderBoth || duplicate.PreviousSchedule != nil || duplicate.PendingScheduleVersionID != "" {
		t.Fatalf("duplicação não promoveu a última escolha a agenda inicial: %#v", duplicate)
	}
	if len(r.versions[duplicate.ID]) != 1 || !SameSchedule(r.versions[duplicate.ID][0].Schedule, duplicate.Schedule) {
		t.Fatalf("snapshot inicial da cópia inválido: %#v", r.versions[duplicate.ID])
	}
}
func TestListsOnlyAuthenticatedUsersHabitsAndFiltersToday(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	monday := validInput()
	monday.Schedule.Weekdays = []int{1}
	monday.Schedule.WeeklyTargetExecutions = 1
	s.Create(context.Background(), userA, "UTC", monday)
	s.Create(context.Background(), userB, "UTC", validInput())
	all, _ := s.List(context.Background(), userA, "UTC", FilterAll)
	if len(all) != 1 || all[0].OwnerUID != "a" {
		t.Fatalf("lista=%#v", all)
	}
	completed, _ := s.List(context.Background(), userA, "UTC", FilterCompleted)
	if len(completed) != 0 {
		t.Fatal("concluídos deve permanecer vazio")
	}
}
func TestSoftDeleteHidesHabitAndPreservesData(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r)
	h, _ := s.Create(context.Background(), userA, "UTC", validInput())
	if err := s.Delete(context.Background(), userA, h.ID); err != nil {
		t.Fatal(err)
	}
	if r.values[h.ID].DeletedAt == nil || len(r.versions[h.ID]) != 1 {
		t.Fatal("exclusão não preservou hábito e snapshot")
	}
	list, _ := s.List(context.Background(), userA, "UTC", FilterAll)
	if len(list) != 0 {
		t.Fatal("hábito excluído apareceu na lista")
	}
}

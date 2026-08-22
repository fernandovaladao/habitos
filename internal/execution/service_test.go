package execution

import (
	"context"
	"errors"
	"habitos/internal/auth"
	"habitos/internal/habit"
	"sync"
	"testing"
	"time"
)

type memoryRepository struct {
	mu      sync.Mutex
	values  map[string]Execution
	keys    map[string]string
	cursors map[string]string
	next    int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{values: map[string]Execution{}, keys: map[string]string{}, cursors: map[string]string{}}
}
func (r *memoryRepository) NewID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	return string(rune('a' + r.next))
}
func (r *memoryRepository) Ensure(_ context.Context, v Execution, key string) (Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.keys[key]; ok {
		return r.values[id], nil
	}
	r.keys[key] = v.ID
	r.values[v.ID] = v
	return v, nil
}
func (r *memoryRepository) Get(_ context.Context, uid, id string) (Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.values[id]
	if !ok || v.OwnerUID != uid {
		return Execution{}, ErrNotFound
	}
	return v, nil
}
func (r *memoryRepository) ListByHabit(_ context.Context, uid, hid, before string, limit int) ([]Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Execution
	for _, v := range r.values {
		if v.OwnerUID == uid && v.HabitID == hid && (before == "" || v.ScheduledDate < before) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *memoryRepository) ApplyResult(_ context.Context, uid, id string, status Status, achieved int64, now time.Time) (Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.values[id]
	if !ok || v.OwnerUID != uid {
		return Execution{}, ErrNotFound
	}
	if v.ClosedAt != nil || !now.Before(v.RegistrationDeadline) {
		return Execution{}, ErrClosed
	}
	if v.Status == status && v.AchievedHundredths == achieved {
		return v, nil
	}
	v.Status = status
	v.AchievedHundredths = achieved
	if status == StatusNotDone {
		v.PerformedAt = nil
	} else if v.PerformedAt == nil {
		p := now
		v.PerformedAt = &p
	}
	v.UpdatedAt = now
	r.values[id] = v
	return v, nil
}
func (r *memoryRepository) CloseExpired(_ context.Context, uid, hid string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, v := range r.values {
		if v.OwnerUID == uid && v.HabitID == hid && v.ClosedAt == nil && !now.Before(v.RegistrationDeadline) {
			if v.Status == StatusPending {
				v.Status = StatusNotDone
			}
			c := v.RegistrationDeadline
			v.ClosedAt = &c
			v.UpdatedAt = now
			r.values[id] = v
		}
	}
	return nil
}
func (r *memoryRepository) Cursor(_ context.Context, uid, hid string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cursors[uid+hid], nil
}
func (r *memoryRepository) AdvanceCursor(_ context.Context, uid, hid, date string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cursors[uid+hid] < date {
		r.cursors[uid+hid] = date
	}
	return nil
}

var identity = auth.Identity{UID: "u1", Email: "u@test"}

func TestRecordRejectsIdentityWithoutUID(t *testing.T) {
	s := serviceAt(newMemoryRepository(), time.Now())
	if _, err := s.RecordSimple(context.Background(), auth.Identity{}, "x", true); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("RecordSimple erro=%v", err)
	}
	if _, err := s.RecordQuantitative(context.Background(), auth.Identity{}, "x", 1); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("RecordQuantitative erro=%v", err)
	}
}

func baseHabit() habit.Habit {
	return habit.Habit{ID: "h1", OwnerUID: "u1", Status: habit.StatusActive, OccurrencesResumeDate: "2026-08-17"}
}
func version(date string, days []int, goal habit.GoalType, target int64) habit.ScheduleVersion {
	return habit.ScheduleVersion{ID: "v" + date, OwnerUID: "u1", HabitID: "h1", EffectiveDate: date, GoalType: goal, TargetHundredths: target, Unit: habit.UnitPages, Schedule: habit.Schedule{Weekdays: days, Time: "19:00", WeeklyTargetExecutions: len(days), Reminder: habit.ReminderNotification}}
}
func serviceAt(r Repository, now time.Time) *Service {
	s := NewService(r)
	s.now = func() time.Time { return now }
	return s
}

func TestOccurrenceOnlyOnScheduledDayAndNotBeforeCreation(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	err := s.SyncHabit(context.Background(), identity, baseHabit(), []habit.ScheduleVersion{version("2026-08-17", []int{1, 3}, habit.GoalSimple, 0)}, "UTC", "2026-08-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.values) != 2 {
		t.Fatalf("execuções=%d", len(r.values))
	}
	for _, v := range r.values {
		if v.ScheduledDate < "2026-08-17" || v.ScheduledDate == "2026-08-18" || v.ScheduledDate == "2026-08-20" {
			t.Fatalf("data indevida %s", v.ScheduledDate)
		}
	}
}
func TestEffectiveDateSelectsHistoricalScheduleAndGoal(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r, time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
	versions := []habit.ScheduleVersion{version("2026-08-17", []int{1, 3, 5}, habit.GoalQuantitative, 1000), version("2026-08-20", []int{4, 5}, habit.GoalQuantitative, 2000)}
	if err := s.SyncHabit(context.Background(), identity, baseHabit(), versions, "UTC", "2026-08-21"); err != nil {
		t.Fatal(err)
	}
	for _, v := range r.values {
		if v.ScheduledDate < "2026-08-20" && v.TargetHundredthsSnapshot != 1000 {
			t.Fatal("meta histórica incorreta")
		}
		if v.ScheduledDate >= "2026-08-20" && v.TargetHundredthsSnapshot != 2000 {
			t.Fatal("meta nova incorreta")
		}
	}
}
func TestRepeatedAndConcurrentSyncDoesNotDuplicate(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r, time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC))
	versions := []habit.ScheduleVersion{version("2026-08-17", []int{1}, habit.GoalSimple, 0)}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.SyncHabit(context.Background(), identity, baseHabit(), versions, "UTC", "2026-08-17")
		}()
	}
	wg.Wait()
	if len(r.values) != 1 {
		t.Fatalf("execuções=%d", len(r.values))
	}
}
func TestSimpleAndQuantitativeRegistration(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	r := newMemoryRepository()
	simple := Execution{ID: "s", OwnerUID: "u1", HabitID: "h1", GoalTypeSnapshot: habit.GoalSimple, Status: StatusPending, RegistrationDeadline: now.Add(24 * time.Hour)}
	quant := Execution{ID: "q", OwnerUID: "u1", HabitID: "h1", GoalTypeSnapshot: habit.GoalQuantitative, TargetHundredthsSnapshot: 2000, Status: StatusPending, RegistrationDeadline: now.Add(24 * time.Hour)}
	r.values["s"] = simple
	r.values["q"] = quant
	s := serviceAt(r, now)
	completed, _ := s.RecordSimple(context.Background(), identity, "s", true)
	if completed.Status != StatusCompleted {
		t.Fatal(completed.Status)
	}
	notDone, _ := s.RecordSimple(context.Background(), identity, "s", false)
	if notDone.Status != StatusNotDone || notDone.PerformedAt != nil {
		t.Fatal("simples não realizado inválido")
	}
	partial, _ := s.RecordQuantitative(context.Background(), identity, "q", 1200)
	if partial.Status != StatusPartial || partial.PerformedAt == nil {
		t.Fatal("parcial inválido")
	}
	performed := partial.PerformedAt
	complete, _ := s.RecordQuantitative(context.Background(), identity, "q", 2000)
	if complete.Status != StatusCompleted || !complete.PerformedAt.Equal(*performed) {
		t.Fatal("correção não preservou performedAt")
	}
	above, _ := s.RecordQuantitative(context.Background(), identity, "q", 2500)
	if above.Status != StatusCompleted {
		t.Fatal("valor superior à meta não concluiu")
	}
	zero, _ := s.RecordQuantitative(context.Background(), identity, "q", 0)
	if zero.Status != StatusNotDone || zero.PerformedAt != nil {
		t.Fatal("zero não virou não realizado")
	}
	later := now.Add(time.Minute)
	s.now = func() time.Time { return later }
	positiveAgain, _ := s.RecordQuantitative(context.Background(), identity, "q", 1000)
	if positiveAgain.Status != StatusPartial || positiveAgain.PerformedAt == nil || !positiveAgain.PerformedAt.Equal(later) {
		t.Fatal("novo resultado positivo não recebeu novo performedAt")
	}
	if _, err := s.RecordQuantitative(context.Background(), identity, "q", -1); !errors.Is(err, ErrInvalidValue) {
		t.Fatal(err)
	}
}
func TestCorrectionWindowAndExpiredClosure(t *testing.T) {
	deadline := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	r := newMemoryRepository()
	r.values["open"] = Execution{ID: "open", OwnerUID: "u1", HabitID: "h1", GoalTypeSnapshot: habit.GoalSimple, Status: StatusPending, RegistrationDeadline: deadline}
	inside := serviceAt(r, deadline.Add(-time.Microsecond))
	if _, err := inside.RecordSimple(context.Background(), identity, "open", true); err != nil {
		t.Fatal(err)
	}
	outside := serviceAt(r, deadline)
	if _, err := outside.RecordSimple(context.Background(), identity, "open", false); !errors.Is(err, ErrClosed) {
		t.Fatalf("erro=%v", err)
	}
	r.values["missed"] = Execution{ID: "missed", OwnerUID: "u1", HabitID: "h1", Status: StatusPending, RegistrationDeadline: deadline}
	_ = outside.SyncHabit(context.Background(), identity, habit.Habit{ID: "h1", OwnerUID: "u1", Status: habit.StatusArchived}, nil, "UTC", "2026-08-19")
	if r.values["missed"].Status != StatusNotDone || r.values["missed"].ClosedAt == nil {
		t.Fatal("vencida não fechou")
	}
}

func TestApplyResultContract(t *testing.T) {
	r := newMemoryRepository()
	deadline := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	performed := deadline.Add(-2 * time.Hour)
	updated := deadline.Add(-3 * time.Hour)
	r.values["result"] = Execution{ID: "result", OwnerUID: "u1", HabitID: "h1", Status: StatusPartial, AchievedHundredths: 1000, PerformedAt: &performed, UpdatedAt: updated, RegistrationDeadline: deadline}
	same, err := r.ApplyResult(context.Background(), "u1", "result", StatusPartial, 1000, deadline.Add(-time.Hour))
	if err != nil || !same.UpdatedAt.Equal(updated) {
		t.Fatalf("resultado idêntico alterou UpdatedAt: %#v erro=%v", same, err)
	}
	completed, err := r.ApplyResult(context.Background(), "u1", "result", StatusCompleted, 2000, deadline.Add(-50*time.Minute))
	if err != nil || completed.PerformedAt == nil || !completed.PerformedAt.Equal(performed) {
		t.Fatal("parcial para concluído não preservou PerformedAt")
	}
	notDone, err := r.ApplyResult(context.Background(), "u1", "result", StatusNotDone, 0, deadline.Add(-40*time.Minute))
	if err != nil || notDone.PerformedAt != nil {
		t.Fatal("not_done não removeu PerformedAt")
	}
	positiveAt := deadline.Add(-30 * time.Minute)
	positive, err := r.ApplyResult(context.Background(), "u1", "result", StatusPartial, 500, positiveAt)
	if err != nil || positive.PerformedAt == nil || !positive.PerformedAt.Equal(positiveAt) {
		t.Fatal("retorno positivo não criou novo PerformedAt")
	}
	if _, err := r.ApplyResult(context.Background(), "u1", "result", StatusCompleted, 2000, deadline); !errors.Is(err, ErrClosed) {
		t.Fatalf("deadline deveria fechar: %v", err)
	}
	closedAt := deadline.Add(-time.Hour)
	r.values["closed"] = Execution{ID: "closed", OwnerUID: "u1", HabitID: "h1", Status: StatusCompleted, ClosedAt: &closedAt, RegistrationDeadline: deadline.Add(time.Hour)}
	if _, err := r.ApplyResult(context.Background(), "u1", "closed", StatusNotDone, 0, deadline); !errors.Is(err, ErrClosed) {
		t.Fatalf("execução fechada aceitou resultado: %v", err)
	}
}

func TestCloseExpiredOnlyClosesMatchingOwnerAndHabit(t *testing.T) {
	r := newMemoryRepository()
	deadline := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	r.values["own"] = Execution{ID: "own", OwnerUID: "u1", HabitID: "h1", Status: StatusPending, RegistrationDeadline: deadline}
	r.values["other-owner"] = Execution{ID: "other-owner", OwnerUID: "u2", HabitID: "h1", Status: StatusPending, RegistrationDeadline: deadline}
	r.values["other-habit"] = Execution{ID: "other-habit", OwnerUID: "u1", HabitID: "h2", Status: StatusPending, RegistrationDeadline: deadline}
	if err := r.CloseExpired(context.Background(), "u1", "h1", deadline); err != nil {
		t.Fatal(err)
	}
	if r.values["own"].ClosedAt == nil || r.values["own"].Status != StatusNotDone {
		t.Fatal("execução correspondente não fechou")
	}
	for _, id := range []string{"other-owner", "other-habit"} {
		if r.values[id].ClosedAt != nil || r.values[id].Status != StatusPending {
			t.Fatalf("%s foi fechada indevidamente", id)
		}
	}
}
func TestArchivedDeletedAndReactivatedMaterialization(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	versions := []habit.ScheduleVersion{version("2026-08-17", []int{1, 2, 3, 4, 5, 6, 7}, habit.GoalSimple, 0)}
	for _, h := range []habit.Habit{{ID: "h1", OwnerUID: "u1", Status: habit.StatusArchived}, {ID: "h1", OwnerUID: "u1", Status: habit.StatusActive, DeletedAt: &now}} {
		r := newMemoryRepository()
		_ = serviceAt(r, now).SyncHabit(context.Background(), identity, h, versions, "UTC", "2026-08-24")
		if len(r.values) != 0 {
			t.Fatal("inativo gerou ocorrência")
		}
	}
	r := newMemoryRepository()
	h := baseHabit()
	h.OccurrencesResumeDate = "2026-08-24"
	_ = serviceAt(r, now).SyncHabit(context.Background(), identity, h, versions, "UTC", "2026-08-24")
	if len(r.values) != 1 {
		t.Fatalf("reativação gerou %d", len(r.values))
	}
}
func TestTimezoneSnapshotIsNotReinterpreted(t *testing.T) {
	r := newMemoryRepository()
	s := serviceAt(r, time.Date(2026, 8, 17, 23, 30, 0, 0, time.UTC))
	versions := []habit.ScheduleVersion{version("2026-08-17", []int{1, 2}, habit.GoalSimple, 0)}
	_ = s.SyncHabit(context.Background(), identity, baseHabit(), versions, "America/Sao_Paulo", "2026-08-17")
	var first Execution
	for _, v := range r.values {
		first = v
	}
	_ = s.SyncHabit(context.Background(), identity, baseHabit(), versions, "Asia/Tokyo", "2026-08-18")
	persisted := r.values[first.ID]
	if persisted.ScheduledDate != "2026-08-17" || persisted.TimezoneSnapshot != "America/Sao_Paulo" || !persisted.RegistrationDeadline.Equal(first.RegistrationDeadline) {
		t.Fatal("timezone reinterpretou ocorrência")
	}
	foundTokyo := false
	for _, v := range r.values {
		if v.ScheduledDate == "2026-08-18" && v.TimezoneSnapshot == "Asia/Tokyo" {
			foundTokyo = true
		}
	}
	if !foundTokyo {
		t.Fatal("mudança de data local não usou novo timezone")
	}
}

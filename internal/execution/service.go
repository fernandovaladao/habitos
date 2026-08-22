package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"habitos/internal/auth"
	"habitos/internal/habit"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) SyncHabit(ctx context.Context, identity auth.Identity, value habit.Habit, versions []habit.ScheduleVersion, timezone, throughDate string) error {
	if identity.UID == "" || value.OwnerUID != identity.UID {
		return auth.ErrInvalidSession
	}
	now := normalize(s.now())
	if err := s.repository.CloseExpired(ctx, identity.UID, value.ID, now); err != nil {
		return err
	}
	if value.Status != habit.StatusActive || value.DeletedAt != nil {
		return nil
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].EffectiveDate < versions[j].EffectiveDate })
	if len(versions) == 0 {
		return ErrNotScheduled
	}
	cursor, err := s.repository.Cursor(ctx, identity.UID, value.ID)
	if err != nil {
		return err
	}
	start := versions[0].EffectiveDate
	if cursor != "" {
		next, err := addDate(cursor, "UTC", 1)
		if err != nil {
			return err
		}
		start = next
	}
	if value.OccurrencesResumeDate > start {
		start = value.OccurrencesResumeDate
	}
	if start > throughDate {
		return nil
	}
	for date := start; date <= throughDate; {
		version, ok := versionForDate(versions, date)
		if ok && scheduledOn(version.Schedule, date) {
			deadline, err := deadlineFor(date, timezone)
			if err != nil {
				return err
			}
			status := StatusPending
			var closedAt *time.Time
			if !now.Before(deadline) {
				status = StatusNotDone
				closed := deadline
				closedAt = &closed
			}
			candidate := Execution{ID: s.repository.NewID(), OwnerUID: identity.UID, HabitID: value.ID, ScheduledDate: date, ScheduleVersionID: version.ID, TimezoneSnapshot: timezone, RegistrationDeadline: deadline, GoalTypeSnapshot: version.GoalType, TargetHundredthsSnapshot: version.TargetHundredths, UnitSnapshot: version.Unit, CustomUnitSnapshot: version.CustomUnit, ScheduleSnapshot: version.Schedule, Status: status, ClosedAt: closedAt, CreatedAt: now, UpdatedAt: now}
			if _, err := s.repository.Ensure(ctx, candidate, uniquenessKey(identity.UID, value.ID, date)); err != nil {
				return err
			}
		}
		next, err := addDate(date, "UTC", 1)
		if err != nil {
			return err
		}
		date = next
	}
	return s.repository.AdvanceCursor(ctx, identity.UID, value.ID, throughDate, now)
}

func (s *Service) RecordSimple(ctx context.Context, identity auth.Identity, id string, completed bool) (Execution, error) {
	if identity.UID == "" {
		return Execution{}, auth.ErrInvalidSession
	}
	value, err := s.repository.Get(ctx, identity.UID, id)
	if err != nil {
		return Execution{}, err
	}
	if value.GoalTypeSnapshot != habit.GoalSimple {
		return Execution{}, ErrInvalidValue
	}
	status := StatusNotDone
	if completed {
		status = StatusCompleted
	}
	return s.repository.ApplyResult(ctx, identity.UID, id, status, 0, normalize(s.now()))
}
func (s *Service) RecordQuantitative(ctx context.Context, identity auth.Identity, id string, achieved int64) (Execution, error) {
	if identity.UID == "" {
		return Execution{}, auth.ErrInvalidSession
	}
	if achieved < 0 {
		return Execution{}, ErrInvalidValue
	}
	value, err := s.repository.Get(ctx, identity.UID, id)
	if err != nil {
		return Execution{}, err
	}
	if value.GoalTypeSnapshot != habit.GoalQuantitative || value.TargetHundredthsSnapshot <= 0 {
		return Execution{}, ErrInvalidValue
	}
	status := StatusNotDone
	if achieved > 0 && achieved < value.TargetHundredthsSnapshot {
		status = StatusPartial
	} else if achieved >= value.TargetHundredthsSnapshot {
		status = StatusCompleted
	}
	return s.repository.ApplyResult(ctx, identity.UID, id, status, achieved, normalize(s.now()))
}
func (s *Service) Get(ctx context.Context, identity auth.Identity, id string) (Execution, error) {
	if identity.UID == "" {
		return Execution{}, auth.ErrInvalidSession
	}
	return s.repository.Get(ctx, identity.UID, id)
}
func (s *Service) History(ctx context.Context, identity auth.Identity, habitID, before string) ([]Execution, error) {
	if identity.UID == "" {
		return nil, auth.ErrInvalidSession
	}
	return s.repository.ListByHabit(ctx, identity.UID, habitID, before, 30)
}

func versionForDate(versions []habit.ScheduleVersion, date string) (habit.ScheduleVersion, bool) {
	index := sort.Search(len(versions), func(i int) bool { return versions[i].EffectiveDate > date })
	if index == 0 {
		return habit.ScheduleVersion{}, false
	}
	return versions[index-1], true
}
func scheduledOn(schedule habit.Schedule, date string) bool {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false
	}
	weekday := int(parsed.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	for _, day := range schedule.Weekdays {
		if day == weekday {
			return true
		}
	}
	return false
}
func deadlineFor(date, timezone string) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return time.Time{}, err
	}
	return normalize(parsed.AddDate(0, 0, 2)), nil
}
func addDate(date, timezone string, days int) (string, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", err
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return "", err
	}
	return parsed.AddDate(0, 0, days).Format("2006-01-02"), nil
}
func uniquenessKey(owner, habitID, date string) string {
	sum := sha256.Sum256([]byte(owner + "\x00" + habitID + "\x00" + date))
	return hex.EncodeToString(sum[:])
}
func normalize(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }

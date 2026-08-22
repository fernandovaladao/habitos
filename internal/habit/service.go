package habit

import (
	"context"
	"sort"
	"strconv"
	"time"

	"habitos/internal/auth"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Create(ctx context.Context, identity auth.Identity, timezone string, input Input) (Habit, error) {
	if identity.UID == "" {
		return Habit{}, auth.ErrInvalidSession
	}
	input = Normalize(input)
	if err := Validate(input); err != nil {
		return Habit{}, err
	}
	now := normalized(s.now())
	effective, err := localDayStart(now, timezone, 0)
	if err != nil {
		return Habit{}, err
	}
	id := s.repository.NewID()
	value := Habit{ID: id, OwnerUID: identity.UID, Title: input.Title, Description: input.Description, Status: StatusActive, GoalType: input.GoalType, TargetHundredths: input.TargetHundredths, Unit: input.Unit, CustomUnit: input.CustomUnit, Schedule: input.Schedule, ScheduleEffectiveAt: effective, OccurrencesResumeAt: effective, CreatedAt: now, UpdatedAt: now}
	version := ScheduleVersion{ID: s.repository.NewID(), HabitID: id, OwnerUID: identity.UID, Schedule: input.Schedule, EffectiveAt: effective, CreatedAt: now}
	if err := s.repository.Create(ctx, value, version); err != nil {
		return Habit{}, err
	}
	return value, nil
}

func (s *Service) Get(ctx context.Context, identity auth.Identity, id string) (Habit, error) {
	if identity.UID == "" {
		return Habit{}, auth.ErrInvalidSession
	}
	return s.repository.Get(ctx, identity.UID, id)
}

func (s *Service) List(ctx context.Context, identity auth.Identity, timezone string, filter ListFilter) ([]Habit, error) {
	if identity.UID == "" {
		return nil, auth.ErrInvalidSession
	}
	values, err := s.repository.List(ctx, identity.UID)
	if err != nil {
		return nil, err
	}
	if filter == FilterCompleted {
		return []Habit{}, nil
	}
	if filter == FilterToday {
		start, err := localDayStart(s.now(), timezone, 0)
		if err != nil {
			return nil, err
		}
		weekday := int(start.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		filtered := values[:0]
		for _, value := range values {
			if value.Status != StatusActive || start.Before(value.OccurrencesResumeAt) {
				continue
			}
			for _, day := range value.EffectiveSchedule(start).Weekdays {
				if day == weekday {
					filtered = append(filtered, value)
					break
				}
			}
		}
		values = filtered
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.After(values[j].CreatedAt) })
	return values, nil
}

func (s *Service) Update(ctx context.Context, identity auth.Identity, timezone, id string, input Input) (Habit, error) {
	if identity.UID == "" {
		return Habit{}, auth.ErrInvalidSession
	}
	input = Normalize(input)
	if err := Validate(input); err != nil {
		return Habit{}, err
	}
	value, err := s.repository.Get(ctx, identity.UID, id)
	if err != nil {
		return Habit{}, err
	}
	now := normalized(s.now())
	var version *ScheduleVersion
	if !SameSchedule(value.Schedule, input.Schedule) {
		effective, err := localDayStart(now, timezone, 1)
		if err != nil {
			return Habit{}, err
		}
		current := value.EffectiveSchedule(now)
		pendingID := pendingVersionID(effective)
		value.PreviousSchedule = &current
		value.Schedule = input.Schedule
		value.ScheduleEffectiveAt = effective
		value.PendingScheduleVersionID = pendingID
		v := ScheduleVersion{ID: pendingID, HabitID: id, OwnerUID: identity.UID, Schedule: input.Schedule, EffectiveAt: effective, CreatedAt: now}
		version = &v
	}
	value.Title = input.Title
	value.Description = input.Description
	value.GoalType = input.GoalType
	value.TargetHundredths = input.TargetHundredths
	value.Unit = input.Unit
	value.CustomUnit = input.CustomUnit
	value.UpdatedAt = now
	if err := s.repository.Update(ctx, value, version); err != nil {
		return Habit{}, err
	}
	return value, nil
}

func (s *Service) Archive(ctx context.Context, identity auth.Identity, id string) (Habit, error) {
	return s.transition(ctx, identity, "", id, StatusArchived)
}
func (s *Service) Reactivate(ctx context.Context, identity auth.Identity, timezone, id string) (Habit, error) {
	return s.transition(ctx, identity, timezone, id, StatusActive)
}
func (s *Service) transition(ctx context.Context, identity auth.Identity, timezone, id string, target Status) (Habit, error) {
	value, err := s.Get(ctx, identity, id)
	if err != nil {
		return Habit{}, err
	}
	now := normalized(s.now())
	if target == StatusArchived {
		if value.Status != StatusActive {
			return Habit{}, ErrInvalidTransition
		}
		value.Status = target
		value.ArchivedAt = &now
	} else {
		if value.Status != StatusArchived {
			return Habit{}, ErrInvalidTransition
		}
		resume, err := localDayStart(now, timezone, 1)
		if err != nil {
			return Habit{}, err
		}
		value.Status = target
		value.ArchivedAt = nil
		value.ReactivatedAt = &now
		value.OccurrencesResumeAt = resume
	}
	value.UpdatedAt = now
	if err := s.repository.Update(ctx, value, nil); err != nil {
		return Habit{}, err
	}
	return value, nil
}

func (s *Service) Delete(ctx context.Context, identity auth.Identity, id string) error {
	value, err := s.Get(ctx, identity, id)
	if err != nil {
		return err
	}
	now := normalized(s.now())
	value.DeletedAt = &now
	value.UpdatedAt = now
	return s.repository.Update(ctx, value, nil)
}

func (s *Service) Duplicate(ctx context.Context, identity auth.Identity, timezone, id string) (Habit, error) {
	value, err := s.Get(ctx, identity, id)
	if err != nil {
		return Habit{}, err
	}
	return s.Create(ctx, identity, timezone, Input{Title: value.Title, Description: value.Description, GoalType: value.GoalType, TargetHundredths: value.TargetHundredths, Unit: value.Unit, CustomUnit: value.CustomUnit, Schedule: value.Schedule})
}

func normalized(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }
func pendingVersionID(effectiveAt time.Time) string {
	return "effective-" + strconv.FormatInt(effectiveAt.UnixMicro(), 10)
}
func localDayStart(value time.Time, timezone string, addDays int) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, err
	}
	local := value.In(location).AddDate(0, 0, addDays)
	return normalized(time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)), nil
}

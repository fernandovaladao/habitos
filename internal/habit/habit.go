package habit

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("hábito não encontrado")
	ErrInvalidInput      = errors.New("dados do hábito inválidos")
	ErrInvalidGoal       = errors.New("meta quantitativa deve ser positiva e ter até 2 casas decimais")
	ErrInvalidSchedule   = errors.New("selecione dias válidos e no máximo uma ocorrência por dia")
	ErrInvalidWeekly     = errors.New("meta semanal não pode ser maior que a quantidade de dias programados")
	ErrInvalidUnit       = errors.New("unidade inválida")
	ErrInvalidReminder   = errors.New("forma de lembrete inválida")
	ErrInvalidTransition = errors.New("transição de estado inválida")
)

type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

type GoalType string

const (
	GoalSimple       GoalType = "simple"
	GoalQuantitative GoalType = "quantitative"
)

type Unit string

const (
	UnitPages      Unit = "pages"
	UnitMinutes    Unit = "minutes"
	UnitKilometers Unit = "kilometers"
	UnitTimes      Unit = "times"
	UnitLiters     Unit = "liters"
	UnitOther      Unit = "other"
)

type ReminderChannel string

const (
	ReminderNotification ReminderChannel = "notification"
	ReminderEmail        ReminderChannel = "email"
	ReminderBoth         ReminderChannel = "both"
)

type Schedule struct {
	Weekdays               []int           `firestore:"weekdays" json:"weekdays"`
	Time                   string          `firestore:"time" json:"time"`
	WeeklyTargetExecutions int             `firestore:"weeklyTargetExecutions" json:"weeklyTargetExecutions"`
	Reminder               ReminderChannel `firestore:"reminder" json:"reminder"`
}

type ScheduleVersion struct {
	ID          string    `firestore:"id" json:"id"`
	HabitID     string    `firestore:"habitId" json:"habitId"`
	OwnerUID    string    `firestore:"ownerUid" json:"-"`
	Schedule    Schedule  `firestore:"schedule" json:"schedule"`
	EffectiveAt time.Time `firestore:"effectiveAt" json:"effectiveAt"`
	CreatedAt   time.Time `firestore:"createdAt" json:"createdAt"`
}

type Habit struct {
	ID                       string     `firestore:"id" json:"id"`
	OwnerUID                 string     `firestore:"ownerUid" json:"-"`
	Title                    string     `firestore:"title" json:"title"`
	Description              string     `firestore:"description" json:"description"`
	Status                   Status     `firestore:"status" json:"status"`
	GoalType                 GoalType   `firestore:"goalType" json:"goalType"`
	TargetHundredths         int64      `firestore:"targetHundredths" json:"targetHundredths"`
	Unit                     Unit       `firestore:"unit" json:"unit"`
	CustomUnit               string     `firestore:"customUnit" json:"customUnit"`
	Schedule                 Schedule   `firestore:"schedule" json:"schedule"`
	PreviousSchedule         *Schedule  `firestore:"previousSchedule,omitempty" json:"previousSchedule,omitempty"`
	ScheduleEffectiveAt      time.Time  `firestore:"scheduleEffectiveAt" json:"scheduleEffectiveAt"`
	PendingScheduleVersionID string     `firestore:"pendingScheduleVersionId,omitempty" json:"-"`
	OccurrencesResumeAt      time.Time  `firestore:"occurrencesResumeAt,omitempty" json:"occurrencesResumeAt,omitempty"`
	CreatedAt                time.Time  `firestore:"createdAt" json:"createdAt"`
	UpdatedAt                time.Time  `firestore:"updatedAt" json:"updatedAt"`
	ArchivedAt               *time.Time `firestore:"archivedAt,omitempty" json:"archivedAt,omitempty"`
	ReactivatedAt            *time.Time `firestore:"reactivatedAt,omitempty" json:"reactivatedAt,omitempty"`
	DeletedAt                *time.Time `firestore:"deletedAt,omitempty" json:"-"`
}

type Input struct {
	Title            string
	Description      string
	GoalType         GoalType
	TargetHundredths int64
	Unit             Unit
	CustomUnit       string
	Schedule         Schedule
}

type ListFilter string

const (
	FilterAll       ListFilter = "all"
	FilterToday     ListFilter = "today"
	FilterCompleted ListFilter = "completed"
)

type Repository interface {
	NewID() string
	Create(ctx context.Context, value Habit, version ScheduleVersion) error
	Get(ctx context.Context, ownerUID, id string) (Habit, error)
	List(ctx context.Context, ownerUID string) ([]Habit, error)
	Update(ctx context.Context, value Habit, version *ScheduleVersion) error
}

func (h Habit) EffectiveSchedule(at time.Time) Schedule {
	if h.PreviousSchedule != nil && at.Before(h.ScheduleEffectiveAt) {
		return *h.PreviousSchedule
	}
	return h.Schedule
}

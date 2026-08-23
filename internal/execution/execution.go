package execution

import (
	"context"
	"errors"
	"time"

	"habitos/internal/gamification"
	"habitos/internal/habit"
)

var (
	ErrNotFound     = errors.New("execução não encontrada")
	ErrNotScheduled = errors.New("não existe ocorrência programada para esta data")
	ErrClosed       = errors.New("a janela de registro desta execução foi encerrada")
	ErrInvalidValue = errors.New("valor realizado inválido")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusPartial   Status = "partial"
	StatusNotDone   Status = "not_done"
)

type Execution struct {
	ID                       string         `firestore:"id" json:"id"`
	OwnerUID                 string         `firestore:"ownerUid" json:"-"`
	HabitID                  string         `firestore:"habitId" json:"habitId"`
	ScheduledDate            string         `firestore:"scheduledDate" json:"scheduledDate"`
	ScheduleVersionID        string         `firestore:"scheduleVersionId" json:"scheduleVersionId"`
	TimezoneSnapshot         string         `firestore:"timezoneSnapshot" json:"timezoneSnapshot"`
	RegistrationDeadline     time.Time      `firestore:"registrationDeadline" json:"registrationDeadline"`
	GoalTypeSnapshot         habit.GoalType `firestore:"goalTypeSnapshot" json:"goalTypeSnapshot"`
	TargetHundredthsSnapshot int64          `firestore:"targetValueSnapshot" json:"targetValueSnapshot"`
	UnitSnapshot             habit.Unit     `firestore:"unitSnapshot" json:"unitSnapshot"`
	CustomUnitSnapshot       string         `firestore:"customUnitSnapshot" json:"customUnitSnapshot"`
	ScheduleSnapshot         habit.Schedule `firestore:"scheduleSnapshot" json:"scheduleSnapshot"`
	Status                   Status         `firestore:"status" json:"status"`
	AchievedHundredths       int64          `firestore:"achievedValue" json:"achievedValue"`
	PointsAwarded            int            `firestore:"pointsAwarded" json:"pointsAwarded"`
	StreakBefore             int            `firestore:"streakBefore" json:"streakBefore"`
	StreakAfter              int            `firestore:"streakAfter" json:"streakAfter"`
	ScoredAt                 *time.Time     `firestore:"scoredAt,omitempty" json:"scoredAt,omitempty"`
	PerformedAt              *time.Time     `firestore:"performedAt,omitempty" json:"performedAt,omitempty"`
	ClosedAt                 *time.Time     `firestore:"closedAt,omitempty" json:"closedAt,omitempty"`
	CreatedAt                time.Time      `firestore:"createdAt" json:"createdAt"`
	UpdatedAt                time.Time      `firestore:"updatedAt" json:"updatedAt"`
}

type Repository interface {
	NewID() string
	Ensure(ctx context.Context, value Execution, uniquenessKey string) (Execution, error)
	Get(ctx context.Context, ownerUID, id string) (Execution, error)
	ListByHabit(ctx context.Context, ownerUID, habitID, beforeDate string, limit int) ([]Execution, error)
	ApplyResult(ctx context.Context, ownerUID, id string, status Status, achieved int64, now time.Time) (Execution, error)
	CloseExpired(ctx context.Context, ownerUID, habitID string, now time.Time) error
	Cursor(ctx context.Context, ownerUID, habitID string) (string, error)
	AdvanceCursor(ctx context.Context, ownerUID, habitID, date string, now time.Time) error
	ReconcileHabit(ctx context.Context, ownerUID, habitID string, now time.Time) error
	Streak(ctx context.Context, ownerUID, habitID string) (gamification.Streak, error)
	Achievements(ctx context.Context, ownerUID string) ([]gamification.UserAchievement, error)
}

package accountdeletion

import (
	"context"
	"errors"
	"time"

	"habitos/internal/auth"
)

const (
	ConfirmationPhrase = "EXCLUIR MINHA CONTA"
	BatchSize          = 200
	MarkerTTL          = 7 * 24 * time.Hour
	LeaseDuration      = time.Minute
)

var (
	ErrInvalidConfirmation = errors.New("confirmação de exclusão inválida")
	ErrNotStarted          = errors.New("exclusão não iniciada")
	ErrIdentityMismatch    = errors.New("identidade de reautenticação divergente")
)

type Stage string

const (
	StageNotes              Stage = "notes"
	StageUniqueness         Stage = "execution_uniqueness"
	StageExecutions         Stage = "executions"
	StageCursors            Stage = "occurrence_cursors"
	StageStreaks            Stage = "streaks"
	StageBonuses            Stage = "bonuses"
	StageAchievements       Stage = "achievements"
	StageSchedules          Stage = "schedule_versions"
	StageHabits             Stage = "habits"
	StageAvatarMedia        Stage = "avatar_media"
	StageAvatarCleanup      Stage = "avatar_cleanup"
	StagePushSubscriptions  Stage = "push_subscriptions"
	StageReminderSchedules  Stage = "reminder_schedules"
	StageReminderDeliveries Stage = "reminder_deliveries"
	StageProfile            Stage = "profile"
	StageStorage            Stage = "storage"
	StageVerify             Stage = "verify"
	StageAuth               Stage = "auth"
)

type State struct {
	Stage      Stage      `firestore:"stage"`
	StartedAt  time.Time  `firestore:"startedAt"`
	UpdatedAt  time.Time  `firestore:"updatedAt"`
	ExpiresAt  *time.Time `firestore:"expiresAt,omitempty"`
	LeaseID    string     `firestore:"leaseId,omitempty"`
	LeaseUntil *time.Time `firestore:"leaseUntil,omitempty"`
}

type Result struct {
	Complete bool   `json:"complete"`
	Stage    string `json:"stage,omitempty"`
}

type Repository interface {
	Begin(context.Context, string, time.Time) (State, error)
	State(context.Context, string) (State, error)
	AcquireLease(context.Context, string, string, time.Time) (State, bool, error)
	ReleaseLease(context.Context, string, string) error
	DeleteBatch(context.Context, string, Stage, int, time.Time) (State, error)
	FunctionalEmpty(context.Context, string) (bool, error)
	SetStage(context.Context, string, Stage, time.Time) (State, error)
	ArmMarkerTTL(context.Context, string, time.Time) error
	RemoveMarker(context.Context, string) error
}

type ObjectStore interface {
	DeleteBatch(context.Context, string, int) (bool, error)
	Empty(context.Context, string) (bool, error)
}

type RecentTokenVerifier interface {
	VerifyRecentIDToken(context.Context, string) (auth.Identity, error)
}

type AccountStore interface {
	RevokeTokens(context.Context, string) error
	DeleteUser(context.Context, string) error
}

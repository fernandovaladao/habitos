package reminder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

const (
	CollectionSchedules     = "reminderSchedules"
	CollectionDeliveries    = "reminderDeliveries"
	CollectionSubscriptions = "pushSubscriptions"
	MaxActiveSubscriptions  = 10
	ProcessBatchSize        = 50
	LeaseDuration           = 2 * time.Minute
	DeliveryLifetime        = 30 * time.Minute
)

var (
	ErrInvalidSubscription = errors.New("subscription de notificação inválida")
	ErrSubscriptionLimit   = errors.New("limite de dispositivos atingido")
	ErrNotFound            = errors.New("lembrete não encontrado")
)

type Channel string

const (
	ChannelNotification Channel = "notification"
	ChannelEmail        Channel = "email"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusSent       Status = "sent"
	StatusSkipped    Status = "skipped"
	StatusFailed     Status = "failed"
)

type PhysicalStatus string

const (
	PhysicalPending  PhysicalStatus = "pending"
	PhysicalSent     PhysicalStatus = "sent"
	PhysicalDisabled PhysicalStatus = "disabled"
	PhysicalFailed   PhysicalStatus = "failed"
)

type Schedule struct {
	HabitID                  string    `firestore:"habitId"`
	OwnerUID                 string    `firestore:"ownerUid"`
	NextScheduledDate        string    `firestore:"nextScheduledDate"`
	NextScheduledAt          time.Time `firestore:"nextScheduledAt"`
	TimezoneSnapshot         string    `firestore:"timezoneSnapshot"`
	ScheduleVersionID        string    `firestore:"scheduleVersionId"`
	Notification             bool      `firestore:"notification"`
	Email                    bool      `firestore:"email"`
	ReconciliationGeneration int64     `firestore:"reconciliationGeneration"`
	UpdatedAt                time.Time `firestore:"updatedAt"`
}

type MessageSnapshot struct {
	HabitTitle    string `firestore:"habitTitle"`
	ScheduledTime string `firestore:"scheduledTime"`
	Recipient     string `firestore:"recipient"`
}

type PhysicalDelivery struct {
	Status       PhysicalStatus `firestore:"status"`
	AttemptCount int            `firestore:"attemptCount"`
	SentAt       *time.Time     `firestore:"sentAt,omitempty"`
	FailureCode  string         `firestore:"failureCode,omitempty"`
}

type Delivery struct {
	ID                        string                      `firestore:"id"`
	OwnerUID                  string                      `firestore:"ownerUid"`
	HabitID                   string                      `firestore:"habitId"`
	ScheduledDate             string                      `firestore:"scheduledDate"`
	Channel                   Channel                     `firestore:"channel"`
	ScheduleVersionIDSnapshot string                      `firestore:"scheduleVersionIdSnapshot"`
	TimezoneSnapshot          string                      `firestore:"timezoneSnapshot"`
	ScheduledAt               time.Time                   `firestore:"scheduledAt"`
	ExpiresAt                 time.Time                   `firestore:"expiresAt"`
	Message                   MessageSnapshot             `firestore:"message"`
	Status                    Status                      `firestore:"status"`
	AttemptCount              int                         `firestore:"attemptCount"`
	NextAttemptAt             time.Time                   `firestore:"nextAttemptAt"`
	LeaseID                   string                      `firestore:"leaseId,omitempty"`
	LeaseUntil                *time.Time                  `firestore:"leaseUntil,omitempty"`
	Physical                  map[string]PhysicalDelivery `firestore:"physical,omitempty"`
	EquivalentTo              string                      `firestore:"equivalentTo,omitempty"`
	FailureCode               string                      `firestore:"failureCode,omitempty"`
	SentAt                    *time.Time                  `firestore:"sentAt,omitempty"`
	SkippedAt                 *time.Time                  `firestore:"skippedAt,omitempty"`
	CreatedAt                 time.Time                   `firestore:"createdAt"`
	UpdatedAt                 time.Time                   `firestore:"updatedAt"`
}

type Subscription struct {
	ID         string     `firestore:"id" json:"id"`
	OwnerUID   string     `firestore:"ownerUid" json:"-"`
	Endpoint   string     `firestore:"endpoint" json:"-"`
	P256DH     string     `firestore:"p256dh" json:"-"`
	Auth       string     `firestore:"auth" json:"-"`
	CreatedAt  time.Time  `firestore:"createdAt" json:"createdAt"`
	UpdatedAt  time.Time  `firestore:"updatedAt" json:"updatedAt"`
	LastUsedAt *time.Time `firestore:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
	DisabledAt *time.Time `firestore:"disabledAt,omitempty" json:"-"`
}

type Eligibility struct {
	Send          bool
	SkipCode      string
	Subscriptions []Subscription
	Message       *MessageSnapshot
}

type Repository interface {
	ReconcileUser(context.Context, string, time.Time, bool) error
	Due(context.Context, time.Time, int) ([]Delivery, error)
	Claim(context.Context, string, string, time.Time) (Delivery, bool, error)
	Revalidate(context.Context, Delivery, time.Time) (Eligibility, error)
	RecordPhysical(context.Context, string, string, PhysicalDelivery, time.Time) error
	Finish(context.Context, string, Status, string, time.Time) error
	Retry(context.Context, string, int, time.Time) error
	UpsertSubscription(context.Context, Subscription) (Subscription, error)
	DisableSubscription(context.Context, string, string, time.Time) error
	ListSubscriptions(context.Context, string) ([]Subscription, error)
}

type EmailMessage struct {
	To             string
	HabitTitle     string
	ScheduledTime  string
	IdempotencyKey string
}

type EmailSender interface {
	SendReminder(context.Context, EmailMessage) error
}

type permanentError interface{ Permanent() bool }

func IsPermanent(err error) bool { value, ok := err.(permanentError); return ok && value.Permanent() }

type PushMessage struct{ Title, Body, URL string }

type PushSender interface {
	Send(context.Context, Subscription, PushMessage) (permanent bool, err error)
}

func DeliveryID(ownerUID, habitID, scheduledDate string, channel Channel) string {
	sum := sha256.Sum256([]byte(ownerUID + "\x00" + habitID + "\x00" + scheduledDate + "\x00" + string(channel)))
	return hex.EncodeToString(sum[:])
}

func SubscriptionID(ownerUID, endpoint string) string {
	sum := sha256.Sum256([]byte(ownerUID + "\x00" + endpoint))
	return hex.EncodeToString(sum[:])
}

func RetryAt(scheduledAt time.Time, attempt int) time.Time {
	switch attempt {
	case 1:
		return scheduledAt.Add(5 * time.Minute)
	case 2:
		return scheduledAt.Add(15 * time.Minute)
	default:
		return scheduledAt.Add(DeliveryLifetime)
	}
}

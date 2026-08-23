package reminder

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"habitos/internal/auth"
)

type Service struct {
	repository Repository
	email      EmailSender
	push       PushSender
	now        func() time.Time
}

func NewService(repository Repository, email EmailSender, push PushSender) *Service {
	return &Service{repository: repository, email: email, push: push, now: time.Now}
}

func (s *Service) Reconcile(ctx context.Context, identity auth.Identity, timezoneChanged bool) error {
	if identity.UID == "" {
		return auth.ErrInvalidSession
	}
	return s.repository.ReconcileUser(ctx, identity.UID, normalizeTime(s.now()), timezoneChanged)
}

func (s *Service) RegisterSubscription(ctx context.Context, identity auth.Identity, endpoint, p256dh, authKey string) (Subscription, error) {
	if identity.UID == "" {
		return Subscription{}, auth.ErrInvalidSession
	}
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || len(endpoint) > 4096 || p256dh == "" || authKey == "" || len(p256dh) > 512 || len(authKey) > 256 {
		return Subscription{}, ErrInvalidSubscription
	}
	now := normalizeTime(s.now())
	return s.repository.UpsertSubscription(ctx, Subscription{ID: SubscriptionID(identity.UID, endpoint), OwnerUID: identity.UID, Endpoint: endpoint, P256DH: p256dh, Auth: authKey, CreatedAt: now, UpdatedAt: now})
}

func (s *Service) DisableSubscription(ctx context.Context, identity auth.Identity, id string) error {
	if identity.UID == "" {
		return auth.ErrInvalidSession
	}
	return s.repository.DisableSubscription(ctx, identity.UID, id, normalizeTime(s.now()))
}

func (s *Service) Subscriptions(ctx context.Context, identity auth.Identity) ([]Subscription, error) {
	if identity.UID == "" {
		return nil, auth.ErrInvalidSession
	}
	return s.repository.ListSubscriptions(ctx, identity.UID)
}

func (s *Service) Process(ctx context.Context) (int, error) {
	now := normalizeTime(s.now())
	values, err := s.repository.Due(ctx, now, ProcessBatchSize)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, candidate := range values {
		lease := randomID()
		value, acquired, err := s.repository.Claim(ctx, candidate.ID, lease, now)
		if err != nil {
			return processed, err
		}
		if !acquired {
			continue
		}
		eligibility, err := s.repository.Revalidate(ctx, value, now)
		if err != nil {
			return processed, err
		}
		if !eligibility.Send {
			if err := s.repository.Finish(ctx, value.ID, StatusSkipped, eligibility.SkipCode, now); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		sent, transient := false, false
		message := value.Message
		if eligibility.Message != nil {
			message = *eligibility.Message
		}
		if value.Channel == ChannelEmail {
			err = s.email.SendReminder(ctx, EmailMessage{To: message.Recipient, HabitTitle: message.HabitTitle, ScheduledTime: message.ScheduledTime, IdempotencyKey: value.ID})
			sent, transient = err == nil, err != nil && !IsPermanent(err)
		} else {
			for _, subscription := range eligibility.Subscriptions {
				physical := value.Physical[subscription.ID]
				if physical.Status == PhysicalSent {
					sent = true
					continue
				}
				if physical.Status == PhysicalDisabled {
					continue
				}
				permanent, pushErr := s.push.Send(ctx, subscription, PushMessage{Title: "Hora do seu hábito", Body: "Abra o HÁBITOS para conferir o que está programado.", URL: "/meus-habitos?filtro=today"})
				physical.AttemptCount++
				if pushErr == nil {
					physical.Status, physical.SentAt, sent = PhysicalSent, &now, true
				} else if permanent {
					physical.Status, physical.FailureCode = PhysicalDisabled, "subscription_invalid"
					_ = s.repository.DisableSubscription(ctx, value.OwnerUID, subscription.ID, now)
				} else {
					physical.Status, physical.FailureCode, transient = PhysicalFailed, "push_transient", true
				}
				if err := s.repository.RecordPhysical(ctx, value.ID, subscription.ID, physical, now); err != nil {
					return processed, err
				}
			}
		}
		if sent && !transient {
			err = s.repository.Finish(ctx, value.ID, StatusSent, "", now)
		} else if now.Before(value.ExpiresAt) && value.AttemptCount < 2 && transient {
			err = s.repository.Retry(ctx, value.ID, value.AttemptCount+1, RetryAt(value.ScheduledAt, value.AttemptCount+1))
		} else if sent {
			err = s.repository.Finish(ctx, value.ID, StatusSent, "partial_device_delivery", now)
		} else {
			err = s.repository.Finish(ctx, value.ID, StatusFailed, "delivery_failed", now)
		}
		if err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func randomID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}
func normalizeTime(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }

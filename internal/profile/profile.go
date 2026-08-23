package profile

import (
	"context"
	"errors"
	"time"

	"habitos/internal/auth"
)

var ErrNotFound = errors.New("perfil não encontrado")

const (
	AvatarDefault = "default"
	AvatarBlue    = "blue"
	AvatarOrange  = "orange"
	AvatarGreen   = "green"
	AvatarPurple  = "purple"
)

type Profile struct {
	UID                         string     `firestore:"id" json:"uid"`
	Email                       string     `firestore:"email" json:"email"`
	Nickname                    string     `firestore:"nickname" json:"nickname"`
	Age                         int        `firestore:"age" json:"age"`
	WeightHundredths            int64      `firestore:"weightHundredths" json:"weightHundredths"`
	HeightHundredths            int64      `firestore:"heightHundredths" json:"heightHundredths"`
	Gender                      string     `firestore:"gender" json:"gender"`
	AvatarType                  string     `firestore:"avatarType" json:"avatarType"`
	PhotoMediaID                string     `firestore:"photoMediaId,omitempty" json:"-"`
	PhotoObjectPath             string     `firestore:"photoObjectPath,omitempty" json:"-"`
	PhotoUpdatedAt              *time.Time `firestore:"photoUpdatedAt,omitempty" json:"-"`
	Timezone                    string     `firestore:"timezone" json:"timezone"`
	RankingOptIn                bool       `firestore:"rankingOptIn" json:"rankingOptIn"`
	ReminderNotificationEnabled bool       `firestore:"reminderNotificationEnabled" json:"reminderNotificationEnabled"`
	ReminderEmailEnabled        bool       `firestore:"reminderEmailEnabled" json:"reminderEmailEnabled"`
	ProfileComplete             bool       `firestore:"profileComplete" json:"profileComplete"`
	TotalPoints                 int64      `firestore:"totalPoints" json:"totalPoints"`
	TotalPointsReachedAt        *time.Time `firestore:"totalPointsReachedAt,omitempty" json:"totalPointsReachedAt,omitempty"`
	CreatedAt                   time.Time  `firestore:"createdAt" json:"createdAt"`
	UpdatedAt                   time.Time  `firestore:"updatedAt" json:"updatedAt"`
}

type Update struct {
	Nickname                    string
	Age                         int
	Timezone                    string
	RankingOptIn                bool
	WeightHundredths            int64
	HeightHundredths            int64
	Gender                      string
	AvatarType                  string
	ReminderNotificationEnabled bool
	ReminderEmailEnabled        bool
}

type Demographics struct {
	Age              int
	WeightHundredths int64
	HeightHundredths int64
	Gender           string
}

type Repository interface {
	Ensure(ctx context.Context, candidate Profile) (Profile, error)
	Get(ctx context.Context, uid string) (Profile, error)
	Update(ctx context.Context, uid string, update Update, updatedAt time.Time) (Profile, error)
	UpdateDemographics(ctx context.Context, uid string, demographics Demographics, updatedAt time.Time) (Profile, error)
}

func (s *Service) UpdateDemographics(ctx context.Context, identity auth.Identity, value Demographics) (Profile, error) {
	if identity.UID == "" || identity.Email == "" {
		return Profile{}, auth.ErrInvalidSession
	}
	value.Gender = NormalizeOptionalText(value.Gender)
	if err := ValidateDemographics(value); err != nil {
		return Profile{}, err
	}
	return s.repository.UpdateDemographics(ctx, identity.UID, value, normalizeFirestoreTimestamp(s.now()))
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) EnsureProfile(ctx context.Context, identity auth.Identity, timezone string) (Profile, error) {
	if identity.UID == "" || identity.Email == "" {
		return Profile{}, auth.ErrInvalidSession
	}
	if ValidateTimezone(timezone) != nil {
		timezone = "UTC"
	}
	now := normalizeFirestoreTimestamp(s.now())
	return s.repository.Ensure(ctx, Profile{
		UID:                         identity.UID,
		Email:                       identity.Email,
		Timezone:                    timezone,
		AvatarType:                  AvatarDefault,
		RankingOptIn:                false,
		ReminderNotificationEnabled: true,
		ReminderEmailEnabled:        true,
		ProfileComplete:             false,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
}

func (s *Service) Get(ctx context.Context, identity auth.Identity) (Profile, error) {
	if identity.UID == "" || identity.Email == "" {
		return Profile{}, auth.ErrInvalidSession
	}
	return s.repository.Get(ctx, identity.UID)
}

func (s *Service) Update(ctx context.Context, identity auth.Identity, update Update) (Profile, error) {
	if identity.UID == "" || identity.Email == "" {
		return Profile{}, auth.ErrInvalidSession
	}
	update.Nickname = NormalizeNickname(update.Nickname)
	if update.AvatarType == "" {
		update.AvatarType = AvatarDefault
	}
	if err := ValidateUpdate(update); err != nil {
		return Profile{}, err
	}
	return s.repository.Update(ctx, identity.UID, update, normalizeFirestoreTimestamp(s.now()))
}

func normalizeFirestoreTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

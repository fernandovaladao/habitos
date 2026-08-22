package profile

import (
	"context"
	"testing"
	"time"

	"habitos/internal/auth"
)

type memoryRepository struct {
	profiles      map[string]Profile
	lastUpdateUID string
	lastUpdatedAt time.Time
	truncateTimes bool
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{profiles: make(map[string]Profile)}
}

func (r *memoryRepository) Ensure(_ context.Context, candidate Profile) (Profile, error) {
	if existing, ok := r.profiles[candidate.UID]; ok {
		return existing, nil
	}
	persisted := candidate
	if r.truncateTimes {
		persisted.CreatedAt = persisted.CreatedAt.Truncate(time.Microsecond)
		persisted.UpdatedAt = persisted.UpdatedAt.Truncate(time.Microsecond)
	}
	r.profiles[candidate.UID] = persisted
	return candidate, nil
}

func (r *memoryRepository) Get(_ context.Context, uid string) (Profile, error) {
	value, ok := r.profiles[uid]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return value, nil
}

func (r *memoryRepository) Update(_ context.Context, uid string, update Update, updatedAt time.Time) (Profile, error) {
	r.lastUpdateUID = uid
	r.lastUpdatedAt = updatedAt
	value, ok := r.profiles[uid]
	if !ok {
		return Profile{}, ErrNotFound
	}
	value.Nickname = update.Nickname
	value.Age = update.Age
	value.Timezone = update.Timezone
	value.RankingOptIn = update.RankingOptIn
	value.WeightHundredths = update.WeightHundredths
	value.HeightHundredths = update.HeightHundredths
	value.Gender = update.Gender
	value.ProfileComplete = true
	value.UpdatedAt = updatedAt
	r.profiles[uid] = value
	return value, nil
}

func (r *memoryRepository) UpdateDemographics(_ context.Context, uid string, update Demographics, updatedAt time.Time) (Profile, error) {
	value, ok := r.profiles[uid]
	if !ok {
		return Profile{}, ErrNotFound
	}
	r.lastUpdateUID = uid
	r.lastUpdatedAt = updatedAt
	value.Age = update.Age
	value.WeightHundredths = update.WeightHundredths
	value.HeightHundredths = update.HeightHundredths
	value.Gender = update.Gender
	value.UpdatedAt = updatedAt
	r.profiles[uid] = value
	return value, nil
}

func TestProfileTimestampsMatchFirestoreRoundTrip(t *testing.T) {
	repository := newMemoryRepository()
	repository.truncateTimes = true
	service := NewService(repository)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 22, 12, 0, 0, 67145795, time.FixedZone("test", -3*60*60))
	}
	identity := auth.Identity{UID: "user-a", Email: "a@example.com"}

	created, err := service.EnsureProfile(context.Background(), identity, "America/Sao_Paulo")
	if err != nil {
		t.Fatalf("primeiro EnsureProfile: %v", err)
	}
	persisted, err := service.EnsureProfile(context.Background(), identity, "America/Sao_Paulo")
	if err != nil {
		t.Fatalf("segundo EnsureProfile: %v", err)
	}

	if created != persisted {
		t.Fatalf("perfil criado difere do round-trip: criado=%#v persistido=%#v", created, persisted)
	}
	if created.CreatedAt.Location() != time.UTC || created.CreatedAt.Nanosecond() != 67145000 {
		t.Fatalf("CreatedAt não foi normalizado: %s", created.CreatedAt)
	}
	if created.UpdatedAt.Location() != time.UTC || created.UpdatedAt.Nanosecond() != 67145000 {
		t.Fatalf("UpdatedAt não foi normalizado: %s", created.UpdatedAt)
	}

	_, err = service.Update(context.Background(), identity, Update{
		Nickname: "Pessoa A", Age: 16, Timezone: "America/Sao_Paulo",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if repository.lastUpdatedAt.Location() != time.UTC || repository.lastUpdatedAt.Nanosecond() != 67145000 {
		t.Fatalf("timestamp de atualização não foi normalizado: %s", repository.lastUpdatedAt)
	}
}

func TestEnsureProfileIsIdempotentAndPrivateByDefault(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository)
	identity := auth.Identity{UID: "user-a", Email: "a@example.com"}

	first, err := service.EnsureProfile(context.Background(), identity, "America/Sao_Paulo")
	if err != nil {
		t.Fatalf("primeiro EnsureProfile: %v", err)
	}
	second, err := service.EnsureProfile(context.Background(), identity, "UTC")
	if err != nil {
		t.Fatalf("segundo EnsureProfile: %v", err)
	}

	if first != second {
		t.Fatalf("perfil foi sobrescrito: primeiro=%#v segundo=%#v", first, second)
	}
	if first.RankingOptIn {
		t.Fatal("rankingOptIn deveria iniciar false")
	}
	if first.ProfileComplete {
		t.Fatal("perfil de recuperação deveria iniciar incompleto")
	}
}

func TestUpdateUsesAuthenticatedUID(t *testing.T) {
	repository := newMemoryRepository()
	repository.profiles["user-a"] = Profile{UID: "user-a", Email: "a@example.com"}
	repository.profiles["user-b"] = Profile{UID: "user-b", Email: "b@example.com", Nickname: "Outro"}
	service := NewService(repository)

	updated, err := service.Update(context.Background(), auth.Identity{UID: "user-a", Email: "a@example.com"}, Update{
		Nickname: "Pessoa A", Age: 16, Timezone: "America/Sao_Paulo", RankingOptIn: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if repository.lastUpdateUID != "user-a" || updated.UID != "user-a" {
		t.Fatalf("UID atualizado = %q", repository.lastUpdateUID)
	}
	if repository.profiles["user-b"].Nickname != "Outro" {
		t.Fatal("perfil de outro usuário foi alterado")
	}
}

func TestEnsureProfileFallsBackToUTCForInvalidTimezone(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository)
	result, err := service.EnsureProfile(context.Background(), auth.Identity{UID: "user-a", Email: "a@example.com"}, "invalido")
	if err != nil {
		t.Fatalf("EnsureProfile: %v", err)
	}
	if result.Timezone != "UTC" {
		t.Fatalf("timezone = %q, esperado UTC", result.Timezone)
	}
}

package accountdeletion

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"habitos/internal/auth"
)

type repositoryStub struct {
	mu            sync.Mutex
	state         State
	started       bool
	empty         bool
	deleteCalls   int
	removeErr     error
	armErr        error
	removeCalls   int
	armCalls      int
	stageErrors   map[Stage]error
	deleteStarted chan struct{}
	deleteRelease chan struct{}
}

func (r *repositoryStub) Begin(_ context.Context, _ string, now time.Time) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		r.state = State{Stage: StageNotes, StartedAt: now, UpdatedAt: now}
		r.started = true
	}
	return r.state, nil
}

func (r *repositoryStub) State(context.Context, string) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return State{}, ErrNotStarted
	}
	return r.state, nil
}

func (r *repositoryStub) AcquireLease(_ context.Context, _ string, leaseID string, now time.Time) (State, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return State{}, false, ErrNotStarted
	}
	if r.state.LeaseUntil != nil && now.Before(*r.state.LeaseUntil) {
		return r.state, false, nil
	}
	until := now.Add(LeaseDuration)
	r.state.LeaseID, r.state.LeaseUntil = leaseID, &until
	return r.state, true, nil
}

func (r *repositoryStub) ReleaseLease(_ context.Context, _ string, leaseID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.LeaseID == leaseID {
		r.state.LeaseID, r.state.LeaseUntil = "", nil
	}
	return nil
}

func (r *repositoryStub) DeleteBatch(_ context.Context, _ string, stage Stage, _ int, now time.Time) (State, error) {
	if r.deleteStarted != nil {
		select {
		case <-r.deleteStarted:
		default:
			close(r.deleteStarted)
		}
		<-r.deleteRelease
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.stageErrors[stage]; err != nil {
		return State{}, err
	}
	r.deleteCalls++
	r.state.Stage = nextStage(stage)
	r.state.UpdatedAt = now
	return r.state, nil
}

func (r *repositoryStub) FunctionalEmpty(context.Context, string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.stageErrors[StageVerify]; err != nil {
		return false, err
	}
	return r.empty, nil
}

func (r *repositoryStub) SetStage(_ context.Context, _ string, stage Stage, now time.Time) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.stageErrors[stage]; err != nil {
		return State{}, err
	}
	r.state.Stage, r.state.UpdatedAt = stage, now
	return r.state, nil
}

func (r *repositoryStub) RemoveMarker(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeCalls++
	if r.removeErr == nil {
		r.started = false
	}
	return r.removeErr
}

func (r *repositoryStub) ArmMarkerTTL(_ context.Context, _ string, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.armCalls++
	if r.armErr != nil {
		return r.armErr
	}
	r.state.ExpiresAt = &expiresAt
	return nil
}

type objectStoreStub struct {
	mu        sync.Mutex
	empty     bool
	deleteErr error
}

func (s *objectStoreStub) DeleteBatch(context.Context, string, int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return false, s.deleteErr
	}
	s.empty = true
	return true, nil
}
func (s *objectStoreStub) Empty(context.Context, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.empty, nil
}

type tokenStub struct {
	identity auth.Identity
	err      error
	calls    int
}

func (s *tokenStub) VerifyRecentIDToken(context.Context, string) (auth.Identity, error) {
	s.calls++
	return s.identity, s.err
}

type accountStoreStub struct {
	mu          sync.Mutex
	revokeCalls int
	deleteCalls int
	deleteErr   error
}

func (s *accountStoreStub) RevokeTokens(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokeCalls++
	return nil
}
func (s *accountStoreStub) DeleteUser(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	return s.deleteErr
}

func newTestService() (*Service, *repositoryStub, *objectStoreStub, *tokenStub, *accountStoreStub) {
	repository := &repositoryStub{empty: true, stageErrors: map[Stage]error{}}
	objects := &objectStoreStub{empty: true}
	tokens := &tokenStub{identity: auth.Identity{UID: "user-1", Email: "user@example.com"}}
	accounts := &accountStoreStub{}
	service := NewService(repository, objects, tokens, accounts)
	service.now = func() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) }
	return service, repository, objects, tokens, accounts
}

func TestStartRequiresLiteralConfirmationAndMatchingRecentIdentity(t *testing.T) {
	service, repository, _, tokens, _ := newTestService()
	identity := auth.Identity{UID: "user-1", Email: "user@example.com"}
	if _, err := service.Start(context.Background(), identity, "EXCLUIR", "token"); !errors.Is(err, ErrInvalidConfirmation) {
		t.Fatalf("Start() erro = %v", err)
	}
	if repository.started {
		t.Fatal("marcador criado sem confirmação literal")
	}
	tokens.identity.UID = "other"
	if _, err := service.Start(context.Background(), identity, ConfirmationPhrase, "token"); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("Start() erro = %v", err)
	}
}

func TestContinuationDoesNotRequireRecentAuthenticationAgain(t *testing.T) {
	service, repository, _, tokens, _ := newTestService()
	identity := auth.Identity{UID: "user-1", Email: "user@example.com"}
	if _, err := service.Start(context.Background(), identity, ConfirmationPhrase, "token"); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 8, 23, 12, 6, 0, 0, time.UTC) }
	repository.mu.Lock()
	repository.state.Stage = StageVerify
	repository.mu.Unlock()
	if _, err := service.Continue(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if tokens.calls != 1 {
		t.Fatalf("verificações recentes = %d, esperado 1", tokens.calls)
	}
}

func TestEveryFunctionalStageCanResumeAfterFailure(t *testing.T) {
	stages := []Stage{StageNotes, StageUniqueness, StageExecutions, StageCursors, StageStreaks, StageBonuses, StageAchievements, StageSchedules, StageHabits, StageAvatarMedia, StageAvatarCleanup, StageProfile}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			service, repository, _, _, _ := newTestService()
			repository.started = true
			repository.state.Stage = stage
			repository.stageErrors[stage] = errors.New("transient")
			identity := auth.Identity{UID: "user-1", Email: "user@example.com"}
			if _, err := service.Continue(context.Background(), identity); err == nil {
				t.Fatal("Continue() deveria falhar")
			}
			delete(repository.stageErrors, stage)
			if _, err := service.Continue(context.Background(), identity); err != nil {
				t.Fatalf("retomada: %v", err)
			}
		})
	}
}

func TestStorageVerifyAndAuthStagesResumeAfterFailure(t *testing.T) {
	identity := auth.Identity{UID: "user-1", Email: "user@example.com"}
	service, repository, objects, _, accounts := newTestService()
	repository.started = true
	repository.state.Stage = StageStorage
	objects.deleteErr = errors.New("storage unavailable")
	if _, err := service.Continue(context.Background(), identity); err == nil {
		t.Fatal("Storage deveria falhar")
	}
	objects.deleteErr = nil
	if _, err := service.Continue(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	repository.stageErrors[StageVerify] = errors.New("firestore unavailable")
	if _, err := service.Continue(context.Background(), identity); err == nil {
		t.Fatal("verificação deveria falhar")
	}
	delete(repository.stageErrors, StageVerify)
	if _, err := service.Continue(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	accounts.deleteErr = errors.New("auth unavailable")
	if _, err := service.Continue(context.Background(), identity); err == nil {
		t.Fatal("Auth deveria falhar")
	}
	accounts.deleteErr = nil
	result, err := service.Continue(context.Background(), identity)
	if err != nil || !result.Complete {
		t.Fatalf("retomada Auth = %+v, %v", result, err)
	}
}

func TestLostDeleteUserResponseCanBeContinuedAsAlreadyDeleted(t *testing.T) {
	service, repository, _, _, accounts := newTestService()
	repository.started = true
	repository.state.Stage = StageAuth
	accounts.deleteErr = errors.New("resposta perdida")
	identity := auth.Identity{UID: "user-1", Email: "user@example.com"}
	if _, err := service.Continue(context.Background(), identity); err == nil {
		t.Fatal("primeira chamada deveria observar falha")
	}
	accounts.deleteErr = auth.ErrUserNotFound
	result, err := service.Continue(context.Background(), identity)
	if err != nil || !result.Complete {
		t.Fatalf("retomada = %+v, %v", result, err)
	}
}

func TestConcurrentContinuationsAreIdempotent(t *testing.T) {
	service, repository, _, _, _ := newTestService()
	repository.started = true
	repository.state.Stage = StageNotes
	repository.deleteStarted = make(chan struct{})
	repository.deleteRelease = make(chan struct{})
	identity := auth.Identity{UID: "user-1", Email: "user@example.com"}
	first := make(chan error, 1)
	go func() { _, err := service.Continue(context.Background(), identity); first <- err }()
	<-repository.deleteStarted
	result, err := service.Continue(context.Background(), identity)
	if err != nil || result.Stage != string(StageNotes) {
		t.Fatalf("continuação concorrente = %+v, %v", result, err)
	}
	repository.mu.Lock()
	if repository.deleteCalls != 0 {
		t.Fatal("segunda continuação executou o estágio protegido")
	}
	repository.mu.Unlock()
	close(repository.deleteRelease)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if repository.deleteCalls != 1 {
		t.Fatalf("execuções do estágio = %d, esperado 1", repository.deleteCalls)
	}
}

func TestAuthMarkerCleanupAlwaysTriesImmediateRemoval(t *testing.T) {
	tests := []struct {
		name       string
		removeErr  error
		armErr     error
		authAbsent bool
		wantMarker bool
		wantTTL    bool
	}{
		{name: "remoção imediata bem-sucedida"},
		{name: "remoção falha com TTL armado", removeErr: errors.New("marker unavailable"), wantMarker: true, wantTTL: true},
		{name: "TTL falha mas remoção é tentada", armErr: errors.New("ttl unavailable")},
		{name: "Auth já ausente mantém idempotência", authAbsent: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, repository, _, _, accounts := newTestService()
			repository.started = true
			repository.state.Stage = StageAuth
			repository.removeErr, repository.armErr = test.removeErr, test.armErr
			if test.authAbsent {
				accounts.deleteErr = auth.ErrUserNotFound
			}
			result, err := service.Continue(context.Background(), auth.Identity{UID: "user-1", Email: "user@example.com"})
			if err != nil || !result.Complete {
				t.Fatalf("Continue() = %+v, %v", result, err)
			}
			if repository.armCalls != 1 || repository.removeCalls != 1 {
				t.Fatalf("arm=%d remove=%d", repository.armCalls, repository.removeCalls)
			}
			if repository.started != test.wantMarker {
				t.Fatalf("marcador presente=%v, esperado %v", repository.started, test.wantMarker)
			}
			if (repository.state.ExpiresAt != nil) != test.wantTTL && test.wantMarker {
				t.Fatalf("TTL presente=%v, esperado %v", repository.state.ExpiresAt != nil, test.wantTTL)
			}
		})
	}
}

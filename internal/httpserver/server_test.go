package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"habitos/internal/accountdeletion"
	"habitos/internal/auth"
	"habitos/internal/avatar"
	"habitos/internal/csrf"
	"habitos/internal/execution"
	"habitos/internal/gamification"
	"habitos/internal/habit"
	"habitos/internal/habitsuggestion"
	"habitos/internal/note"
	"habitos/internal/profile"
	"habitos/internal/progress"
	"habitos/internal/ranking"
	"habitos/internal/reminder"
)

type fakeSessionManager struct {
	identity        auth.Identity
	verifyErr       error
	createdDuration time.Duration
	createdIDToken  string
}

type fakeAccountState struct{ deleting bool }

type fakeReminderService struct {
	subscriptions   []reminder.Subscription
	reconciliations int
	timezoneChanged bool
	processed       int
}

func (f *fakeReminderService) Reconcile(_ context.Context, _ auth.Identity, changed bool) error {
	f.reconciliations++
	f.timezoneChanged = changed
	return nil
}
func (f *fakeReminderService) RegisterSubscription(_ context.Context, identity auth.Identity, endpoint, p256dh, authKey string) (reminder.Subscription, error) {
	value := reminder.Subscription{ID: reminder.SubscriptionID(identity.UID, endpoint), OwnerUID: identity.UID, Endpoint: endpoint, P256DH: p256dh, Auth: authKey}
	f.subscriptions = append(f.subscriptions, value)
	return value, nil
}
func (f *fakeReminderService) DisableSubscription(_ context.Context, _ auth.Identity, id string) error {
	for index := range f.subscriptions {
		if f.subscriptions[index].ID == id {
			f.subscriptions = append(f.subscriptions[:index], f.subscriptions[index+1:]...)
			break
		}
	}
	return nil
}
func (f *fakeReminderService) Subscriptions(context.Context, auth.Identity) ([]reminder.Subscription, error) {
	return append([]reminder.Subscription(nil), f.subscriptions...), nil
}
func (f *fakeReminderService) Process(context.Context) (int, error) { return f.processed, nil }

func (f *fakeAccountState) IsDeleting(context.Context, string) (bool, error) { return f.deleting, nil }

type fakeAccountDeletion struct {
	startResult    accountdeletion.Result
	continueResult accountdeletion.Result
	err            error
	identity       auth.Identity
	confirmation   string
	idToken        string
}

func (f *fakeAccountDeletion) Start(_ context.Context, identity auth.Identity, confirmation, idToken string) (accountdeletion.Result, error) {
	f.identity, f.confirmation, f.idToken = identity, confirmation, idToken
	return f.startResult, f.err
}
func (f *fakeAccountDeletion) Continue(_ context.Context, identity auth.Identity) (accountdeletion.Result, error) {
	f.identity = identity
	return f.continueResult, f.err
}

func (f *fakeSessionManager) CreateSession(_ context.Context, idToken string, duration time.Duration) (string, error) {
	f.createdIDToken = idToken
	f.createdDuration = duration
	return "firebase-session-cookie", nil
}

func (f *fakeSessionManager) VerifySession(_ context.Context, _ string) (auth.Identity, error) {
	return f.identity, f.verifyErr
}

type fakeProfileRepository struct {
	profiles    map[string]profile.Profile
	ensureCalls int
	lastUID     string
}

func newFakeProfileRepository() *fakeProfileRepository {
	return &fakeProfileRepository{profiles: make(map[string]profile.Profile)}
}

func (r *fakeProfileRepository) Ensure(_ context.Context, candidate profile.Profile) (profile.Profile, error) {
	r.ensureCalls++
	if existing, ok := r.profiles[candidate.UID]; ok {
		return existing, nil
	}
	r.profiles[candidate.UID] = candidate
	return candidate, nil
}

func (r *fakeProfileRepository) Get(_ context.Context, uid string) (profile.Profile, error) {
	value, ok := r.profiles[uid]
	if !ok {
		return profile.Profile{}, profile.ErrNotFound
	}
	return value, nil
}

func (r *fakeProfileRepository) Update(_ context.Context, uid string, update profile.Update, updatedAt time.Time) (profile.Profile, error) {
	r.lastUID = uid
	value, ok := r.profiles[uid]
	if !ok {
		return profile.Profile{}, profile.ErrNotFound
	}
	value.Nickname = update.Nickname
	value.Age = update.Age
	value.Timezone = update.Timezone
	value.RankingOptIn = update.RankingOptIn
	value.WeightHundredths = update.WeightHundredths
	value.HeightHundredths = update.HeightHundredths
	value.Gender = update.Gender
	value.AvatarType = update.AvatarType
	value.ReminderNotificationEnabled = update.ReminderNotificationEnabled
	value.ReminderEmailEnabled = update.ReminderEmailEnabled
	value.ProfileComplete = true
	value.UpdatedAt = updatedAt
	r.profiles[uid] = value
	return value, nil
}

func (r *fakeProfileRepository) UpdateDemographics(_ context.Context, uid string, update profile.Demographics, updatedAt time.Time) (profile.Profile, error) {
	r.lastUID = uid
	value, ok := r.profiles[uid]
	if !ok {
		return profile.Profile{}, profile.ErrNotFound
	}
	value.Age = update.Age
	value.WeightHundredths = update.WeightHundredths
	value.HeightHundredths = update.HeightHundredths
	value.Gender = update.Gender
	value.UpdatedAt = updatedAt
	r.profiles[uid] = value
	return value, nil
}

type testApp struct {
	handler      http.Handler
	sessions     *fakeSessionManager
	profiles     *fakeProfileRepository
	habits       *fakeHabitRepository
	executions   *fakeExecutionRepository
	notes        *fakeNoteRepository
	ranking      *fakeRankingRepository
	suggestions  *fakeSuggestionProvider
	avatarRepo   *fakeAvatarRepository
	avatarStore  *fakeAvatarStore
	deletion     *fakeAccountDeletion
	accountState *fakeAccountState
	reminders    *fakeReminderService
}

type fakeAvatarRepository struct {
	profiles *fakeProfileRepository
	media    map[string]avatar.Media
}

func (r *fakeAvatarRepository) ReplacePhoto(_ context.Context, uid string, media avatar.Media, now time.Time) (string, error) {
	value, ok := r.profiles.profiles[uid]
	if !ok {
		return "", profile.ErrNotFound
	}
	old := value.PhotoObjectPath
	value.PhotoMediaID, value.PhotoObjectPath, value.PhotoUpdatedAt = media.ID, media.ObjectPath, &now
	r.profiles.profiles[uid] = value
	r.media[media.ID] = media
	return old, nil
}
func (r *fakeAvatarRepository) RemovePhoto(_ context.Context, uid, internalType string, now time.Time) (string, error) {
	value, ok := r.profiles.profiles[uid]
	if !ok {
		return "", profile.ErrNotFound
	}
	old := value.PhotoObjectPath
	delete(r.media, value.PhotoMediaID)
	value.PhotoMediaID, value.PhotoObjectPath, value.PhotoUpdatedAt = "", "", nil
	if internalType != "" {
		value.AvatarType = internalType
	}
	value.UpdatedAt = now
	r.profiles.profiles[uid] = value
	return old, nil
}
func (r *fakeAvatarRepository) Media(_ context.Context, id string) (avatar.Media, error) {
	value, ok := r.media[id]
	if !ok {
		return avatar.Media{}, avatar.ErrNotFound
	}
	return value, nil
}
func (r *fakeAvatarRepository) CurrentMedia(_ context.Context, uid string) (avatar.Media, error) {
	profileValue, ok := r.profiles.profiles[uid]
	if !ok || profileValue.PhotoMediaID == "" {
		return avatar.Media{}, avatar.ErrNotFound
	}
	return r.Media(context.Background(), profileValue.PhotoMediaID)
}
func (*fakeAvatarRepository) CleanupCompleted(context.Context, string) error { return nil }

type fakeAvatarStore struct{ values map[string][]byte }

func (s *fakeAvatarStore) Write(_ context.Context, path string, value []byte) error {
	s.values[path] = append([]byte(nil), value...)
	return nil
}
func (s *fakeAvatarStore) Read(_ context.Context, path string) ([]byte, error) {
	value, ok := s.values[path]
	if !ok {
		return nil, avatar.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (s *fakeAvatarStore) Delete(_ context.Context, path string) error {
	delete(s.values, path)
	return nil
}

type fakeHabitRepository struct {
	values   map[string]habit.Habit
	versions map[string][]habit.ScheduleVersion
	next     int
}

type fakeExecutionRepository struct {
	values  map[string]execution.Execution
	keys    map[string]string
	cursors map[string]string
	next    int
}

func newFakeExecutionRepository() *fakeExecutionRepository {
	return &fakeExecutionRepository{values: map[string]execution.Execution{}, keys: map[string]string{}, cursors: map[string]string{}}
}
func (r *fakeExecutionRepository) NewID() string {
	r.next++
	return fmt.Sprintf("execution-%d", r.next)
}
func (r *fakeExecutionRepository) Ensure(_ context.Context, v execution.Execution, key string) (execution.Execution, error) {
	if id, ok := r.keys[key]; ok {
		return r.values[id], nil
	}
	r.keys[key] = v.ID
	r.values[v.ID] = v
	return v, nil
}
func (r *fakeExecutionRepository) Get(_ context.Context, uid, id string) (execution.Execution, error) {
	v, ok := r.values[id]
	if !ok || v.OwnerUID != uid {
		return execution.Execution{}, execution.ErrNotFound
	}
	return v, nil
}
func (r *fakeExecutionRepository) ListByHabit(_ context.Context, uid, hid, before string, limit int) ([]execution.Execution, error) {
	var out []execution.Execution
	for _, v := range r.values {
		if v.OwnerUID == uid && v.HabitID == hid && (before == "" || v.ScheduledDate < before) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *fakeExecutionRepository) ApplyResult(_ context.Context, uid, id string, status execution.Status, achieved int64, now time.Time) (execution.Execution, error) {
	v, err := r.Get(context.Background(), uid, id)
	if err != nil {
		return execution.Execution{}, err
	}
	if v.ClosedAt != nil || !now.Before(v.RegistrationDeadline) {
		return execution.Execution{}, execution.ErrClosed
	}
	v.Status = status
	v.AchievedHundredths = achieved
	if status == execution.StatusNotDone {
		v.PerformedAt = nil
	} else if v.PerformedAt == nil {
		v.PerformedAt = &now
	}
	v.UpdatedAt = now
	r.values[id] = v
	return v, nil
}
func (r *fakeExecutionRepository) CloseExpired(_ context.Context, uid, hid string, now time.Time) error {
	return nil
}
func (r *fakeExecutionRepository) Cursor(_ context.Context, uid, hid string) (string, error) {
	return r.cursors[uid+hid], nil
}
func (r *fakeExecutionRepository) AdvanceCursor(_ context.Context, uid, hid, date string, _ time.Time) error {
	r.cursors[uid+hid] = date
	return nil
}
func (r *fakeExecutionRepository) ReconcileHabit(context.Context, string, string, time.Time) error {
	return nil
}
func (r *fakeExecutionRepository) Streak(_ context.Context, uid, hid string) (gamification.Streak, error) {
	return gamification.Streak{OwnerUID: uid, HabitID: hid}, nil
}
func (r *fakeExecutionRepository) Achievements(context.Context, string) ([]gamification.UserAchievement, error) {
	return nil, nil
}

type fakeNoteRepository struct {
	values map[string]note.Note
	next   int
}

type fakeProgressRepository struct{}

func (*fakeProgressRepository) Executions(context.Context, string, string, string) ([]execution.Execution, error) {
	return nil, nil
}
func (*fakeProgressRepository) BonusAwards(context.Context, string, string, string) ([]gamification.BonusAward, error) {
	return nil, nil
}
func (*fakeProgressRepository) Streaks(context.Context, string) ([]gamification.Streak, error) {
	return nil, nil
}
func (*fakeProgressRepository) Achievements(context.Context, string) ([]gamification.UserAchievement, error) {
	return nil, nil
}
func (*fakeProgressRepository) Habits(context.Context, string, []string) (map[string]progress.HabitDescriptor, error) {
	return map[string]progress.HabitDescriptor{}, nil
}

type fakeRankingRepository struct {
	top      []ranking.Entry
	self     ranking.Entry
	count    int
	previous *ranking.Entry
}

type fakeSuggestionProvider struct {
	result habitsuggestion.ProviderSuggestion
	err    error
	calls  int
	input  habitsuggestion.ProviderRequest
}

func (f *fakeSuggestionProvider) Suggest(_ context.Context, input habitsuggestion.ProviderRequest) (habitsuggestion.ProviderSuggestion, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

func (r *fakeRankingRepository) Top(_ context.Context, limit int) ([]ranking.Entry, error) {
	if len(r.top) < limit {
		limit = len(r.top)
	}
	return r.top[:limit], nil
}
func (r *fakeRankingRepository) Get(_ context.Context, uid string) (ranking.Entry, error) {
	if r.self.UID != uid {
		return ranking.Entry{}, ranking.ErrNotFound
	}
	return r.self, nil
}
func (r *fakeRankingRepository) CountBefore(context.Context, ranking.Entry) (int, error) {
	return r.count, nil
}
func (r *fakeRankingRepository) Previous(context.Context, ranking.Entry) (*ranking.Entry, error) {
	return r.previous, nil
}

func newFakeNoteRepository() *fakeNoteRepository {
	return &fakeNoteRepository{values: map[string]note.Note{}}
}
func (r *fakeNoteRepository) NewID() string { r.next++; return fmt.Sprintf("note-%d", r.next) }
func (r *fakeNoteRepository) Create(_ context.Context, v note.Note) (note.Note, error) {
	r.values[v.ID] = v
	return v, nil
}
func (r *fakeNoteRepository) Get(_ context.Context, uid, id string) (note.Note, error) {
	v, ok := r.values[id]
	if !ok || v.OwnerUID != uid {
		return note.Note{}, note.ErrNotFound
	}
	return v, nil
}
func (r *fakeNoteRepository) ListByHabit(_ context.Context, uid, hid string) ([]note.Note, error) {
	var out []note.Note
	for _, v := range r.values {
		if v.OwnerUID == uid && v.HabitID == hid {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *fakeNoteRepository) Update(_ context.Context, uid, id, content string, now time.Time) (note.Note, error) {
	v, err := r.Get(context.Background(), uid, id)
	if err != nil {
		return note.Note{}, err
	}
	v.Content = content
	v.UpdatedAt = now
	r.values[id] = v
	return v, nil
}
func (r *fakeNoteRepository) Delete(_ context.Context, uid, id string) error {
	if _, err := r.Get(context.Background(), uid, id); err != nil {
		return err
	}
	delete(r.values, id)
	return nil
}

func newFakeHabitRepository() *fakeHabitRepository {
	return &fakeHabitRepository{values: map[string]habit.Habit{}, versions: map[string][]habit.ScheduleVersion{}}
}
func (r *fakeHabitRepository) NewID() string { r.next++; return fmt.Sprintf("habit-%d", r.next) }
func (r *fakeHabitRepository) Create(_ context.Context, value habit.Habit, version habit.ScheduleVersion) error {
	r.values[value.ID] = value
	r.versions[value.ID] = append(r.versions[value.ID], version)
	return nil
}
func (r *fakeHabitRepository) Get(_ context.Context, uid, id string) (habit.Habit, error) {
	value, ok := r.values[id]
	if !ok || value.OwnerUID != uid || value.DeletedAt != nil {
		return habit.Habit{}, habit.ErrNotFound
	}
	return value, nil
}
func (r *fakeHabitRepository) List(_ context.Context, uid string) ([]habit.Habit, error) {
	var values []habit.Habit
	for _, value := range r.values {
		if value.OwnerUID == uid && value.DeletedAt == nil {
			values = append(values, value)
		}
	}
	return values, nil
}
func (r *fakeHabitRepository) ListScheduleVersions(_ context.Context, uid, id string) ([]habit.ScheduleVersion, error) {
	if _, err := r.Get(context.Background(), uid, id); err != nil {
		return nil, err
	}
	return append([]habit.ScheduleVersion(nil), r.versions[id]...), nil
}
func (r *fakeHabitRepository) Update(_ context.Context, value habit.Habit, version *habit.ScheduleVersion) error {
	persisted, ok := r.values[value.ID]
	if !ok || persisted.OwnerUID != value.OwnerUID || persisted.DeletedAt != nil {
		return habit.ErrNotFound
	}
	if version != nil {
		if persisted.PendingScheduleVersionID == version.ID && persisted.ScheduleEffectiveAt.Equal(version.EffectiveAt) {
			for index := range r.versions[value.ID] {
				if r.versions[value.ID][index].ID == version.ID {
					r.versions[value.ID][index] = *version
					r.values[value.ID] = value
					return nil
				}
			}
			return habit.ErrInvalidTransition
		}
		r.versions[value.ID] = append(r.versions[value.ID], *version)
	}
	r.values[value.ID] = value
	return nil
}

func newTestApp(t *testing.T) testApp {
	t.Helper()
	sessions := &fakeSessionManager{identity: auth.Identity{UID: "firebase-user-a", Email: "a@example.com"}}
	profiles := newFakeProfileRepository()
	habits := newFakeHabitRepository()
	executionRepo := newFakeExecutionRepository()
	executionService := execution.NewService(executionRepo)
	habitService := habit.NewService(habits)
	noteRepo := newFakeNoteRepository()
	rankingRepo := &fakeRankingRepository{}
	suggestionProvider := &fakeSuggestionProvider{result: habitsuggestion.ProviderSuggestion{Title: "Ler um pouco", Description: "Comece com uma meta realista.", GoalType: "quantitative", Target: "10", Unit: "pages", Weekdays: []int{1, 3, 5}, WeeklyTargetExecutions: 3}}
	avatarRepo := &fakeAvatarRepository{profiles: profiles, media: map[string]avatar.Media{}}
	avatarStore := &fakeAvatarStore{values: map[string][]byte{}}
	deletion := &fakeAccountDeletion{}
	accountState := &fakeAccountState{}
	reminders := &fakeReminderService{}
	notes := note.NewService(noteRepo, habitService, executionService)
	handler, err := NewHandler(Config{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		FirebaseWeb: FirebaseWebConfig{APIKey: "public-key", ProjectID: "test-project"},
	}, Dependencies{Sessions: sessions, Profiles: profile.NewService(profiles), Habits: habitService, Executions: executionService, Notes: notes, Progress: progress.NewService(&fakeProgressRepository{}), Ranking: ranking.NewService(rankingRepo), Suggestions: habitsuggestion.NewService(suggestionProvider, 10*time.Second), Avatars: avatar.NewService(avatarRepo, avatarStore), Deletion: deletion, AccountState: accountState, Reminders: reminders})
	if err != nil {
		t.Fatalf("NewHandler() retornou erro: %v", err)
	}
	return testApp{handler: handler, sessions: sessions, profiles: profiles, habits: habits, executions: executionRepo, notes: noteRepo, ranking: rankingRepo, suggestions: suggestionProvider, avatarRepo: avatarRepo, avatarStore: avatarStore, deletion: deletion, accountState: accountState, reminders: reminders}
}

func TestExecutionAndNoteEndpointsDoNotUseAnotherUsersData(t *testing.T) {
	app := newTestApp(t)
	deadline := time.Now().Add(time.Hour)
	app.executions.values["execution-b"] = execution.Execution{ID: "execution-b", OwnerUID: "firebase-user-b", HabitID: "habit-b", GoalTypeSnapshot: habit.GoalSimple, Status: execution.StatusPending, RegistrationDeadline: deadline}
	app.notes.values["note-b"] = note.Note{ID: "note-b", OwnerUID: "firebase-user-b", HabitID: "habit-b", Content: "privada"}
	token, cookie := issueCSRF(t, app.handler)
	session := &http.Cookie{Name: auth.SessionCookieName, Value: "valid"}
	for _, item := range []struct{ method, path, body string }{{"POST", "/api/executions/execution-b/simple", `{"completed":true}`}, {"PUT", "/api/notes/note-b", `{"content":"invadir"}`}, {"DELETE", "/api/notes/note-b", `{}`}} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		request.Header.Set(csrf.HeaderName, token)
		request.AddCookie(cookie)
		request.AddCookie(session)
		app.handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d", item.method, item.path, recorder.Code)
		}
	}
	if app.notes.values["note-b"].Content != "privada" || app.executions.values["execution-b"].Status != execution.StatusPending {
		t.Fatal("dados de outro usuário foram alterados")
	}
}

func TestHabitDetailsSynchronizesBeforeReturningHistory(t *testing.T) {
	app := newTestApp(t)
	today := todayIn("UTC")
	app.habits.values["habit-a"] = habit.Habit{ID: "habit-a", OwnerUID: "firebase-user-a", Title: "Diário", Description: "Teste", Status: habit.StatusActive, ScheduleEffectiveDate: today, OccurrencesResumeDate: today}
	app.habits.versions["habit-a"] = []habit.ScheduleVersion{{ID: "version-a", HabitID: "habit-a", OwnerUID: "firebase-user-a", EffectiveDate: today, GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "08:00", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification}}}
	request := httptest.NewRequest(http.MethodGet, "/habitos/habit-a", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
	if len(app.executions.values) != 1 {
		t.Fatalf("histórico retornado sem sincronizar: execuções=%d", len(app.executions.values))
	}
	for _, value := range app.executions.values {
		if value.ScheduledDate != today {
			t.Fatalf("data materializada=%q", value.ScheduledDate)
		}
	}
}

func TestHabitCreationAndListingUseAuthenticatedUID(t *testing.T) {
	app := newTestApp(t)
	token, csrfCookie := issueCSRF(t, app.handler)
	requestBody := `{"title":"Ler","description":"Ler diariamente","goalType":"quantitative","target":"20.00","unit":"pages","customUnit":"","weekdays":[1,3,5],"time":"19:00","weeklyTargetExecutions":3,"reminder":"notification","age":16,"weight":"60.50","height":"170","gender":""}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/habits?userId=firebase-user-b", strings.NewReader(requestBody))
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(csrfCookie)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("criação status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
	for _, value := range app.habits.values {
		if value.OwnerUID != "firebase-user-a" {
			t.Fatalf("ownerUID=%q", value.OwnerUID)
		}
		if value.TargetHundredths != 2000 {
			t.Fatalf("quantidade persistida=%d", value.TargetHundredths)
		}
	}
	userProfile := app.profiles.profiles["firebase-user-a"]
	if userProfile.WeightHundredths != 6050 || userProfile.HeightHundredths != 17000 {
		t.Fatalf("dados do perfil não foram atualizados: %#v", userProfile)
	}
	app.habits.values["other"] = habit.Habit{ID: "other", OwnerUID: "firebase-user-b", Title: "Segredo", Status: habit.StatusActive}
	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/meus-habitos", nil)
	listRequest.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK || strings.Contains(listRecorder.Body.String(), "Segredo") {
		t.Fatalf("listagem vazou hábito: status=%d", listRecorder.Code)
	}
}

func TestHabitSuggestionRequiresSessionCSRFAndSendsOnlyFormText(t *testing.T) {
	app := newTestApp(t)
	body := `{"title":"Ler","description":"Criar uma rotina"}`
	anonymous := httptest.NewRecorder()
	app.handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodPost, "/api/habit-suggestions", strings.NewReader(body)))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anônimo status=%d", anonymous.Code)
	}

	withoutCSRF := httptest.NewRecorder()
	withoutRequest := httptest.NewRequest(http.MethodPost, "/api/habit-suggestions", strings.NewReader(body))
	withoutRequest.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(withoutCSRF, withoutRequest)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("sem CSRF status=%d", withoutCSRF.Code)
	}

	token, csrfCookie := issueCSRF(t, app.handler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/habit-suggestions", strings.NewReader(body))
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(csrfCookie)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || app.suggestions.calls != 1 || app.suggestions.input != (habitsuggestion.ProviderRequest{Title: "Ler", Description: "Criar uma rotina"}) {
		t.Fatalf("status=%d entrada=%#v chamadas=%d corpo=%s", recorder.Code, app.suggestions.input, app.suggestions.calls, recorder.Body.String())
	}
	for _, forbidden := range []string{"forjado", "privado@example.test", "firebase-user-a", "a@example.com"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("resposta expôs %q: %s", forbidden, recorder.Body.String())
		}
	}

	unknown := httptest.NewRecorder()
	unknownRequest := httptest.NewRequest(http.MethodPost, "/api/habit-suggestions", strings.NewReader(`{"title":"Ler","description":"Criar uma rotina","userId":"forjado"}`))
	unknownRequest.Header.Set(csrf.HeaderName, token)
	unknownRequest.AddCookie(csrfCookie)
	unknownRequest.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(unknown, unknownRequest)
	if unknown.Code != http.StatusBadRequest || app.suggestions.calls != 1 {
		t.Fatalf("campo de identidade inesperado foi aceito: status=%d chamadas=%d", unknown.Code, app.suggestions.calls)
	}
}

func TestHabitSuggestionReturnsGenericProviderError(t *testing.T) {
	app := newTestApp(t)
	app.suggestions.err = errors.New("modelo secreto e corpo do provedor")
	token, csrfCookie := issueCSRF(t, app.handler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/habit-suggestions", strings.NewReader(`{"title":"Ler","description":"Criar rotina"}`))
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(csrfCookie)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "modelo secreto") || strings.Contains(recorder.Body.String(), "provedor") {
		t.Fatalf("status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAISuggestionButtonAppearsOnlyWhenCreatingHabit(t *testing.T) {
	app := newTestApp(t)
	session := &http.Cookie{Name: auth.SessionCookieName, Value: "valid"}
	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodGet, "/criar-habito", nil)
	createRequest.AddCookie(session)
	app.handler.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusOK || !strings.Contains(createRecorder.Body.String(), "Sugerir hábito com IA") {
		t.Fatalf("criação status=%d", createRecorder.Code)
	}
	app.habits.values["habit-a"] = habit.Habit{ID: "habit-a", OwnerUID: "firebase-user-a", Title: "Ler", Description: "Livros", Status: habit.StatusActive}
	editRecorder := httptest.NewRecorder()
	editRequest := httptest.NewRequest(http.MethodGet, "/habitos/habit-a/editar", nil)
	editRequest.AddCookie(session)
	app.handler.ServeHTTP(editRecorder, editRequest)
	if editRecorder.Code != http.StatusOK || strings.Contains(editRecorder.Body.String(), "Sugerir hábito com IA") {
		t.Fatalf("edição status=%d corpo=%s", editRecorder.Code, editRecorder.Body.String())
	}
}

func TestHabitRoutesHideAnotherUsersHabit(t *testing.T) {
	app := newTestApp(t)
	app.habits.values["habit-b"] = habit.Habit{ID: "habit-b", OwnerUID: "firebase-user-b", Title: "Privado", Status: habit.StatusActive}
	request := httptest.NewRequest(http.MethodGet, "/habitos/habit-b", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("detalhe status=%d", recorder.Code)
	}
	token, cookie := issueCSRF(t, app.handler)
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/habits/habit-b", strings.NewReader(`{}`))
	deleteRequest.Header.Set(csrf.HeaderName, token)
	deleteRequest.AddCookie(cookie)
	deleteRequest.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	deleteRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusNotFound || app.habits.values["habit-b"].DeletedAt != nil {
		t.Fatalf("exclusão status=%d", deleteRecorder.Code)
	}
}

func TestParseDecimalFieldsExactly(t *testing.T) {
	for input, want := range map[string]int64{"1": 100, "1,2": 120, "1.23": 123, "": 0} {
		got, err := parseOptionalHundredths(input)
		if err != nil || got != want {
			t.Fatalf("%q=%d erro=%v; esperado %d", input, got, err, want)
		}
	}
	for _, input := range []string{"0.001", "-1", "abc"} {
		if _, err := parseOptionalHundredths(input); err == nil {
			t.Fatalf("%q deveria ser inválido", input)
		}
	}
	for _, input := range []string{"0", "0.00"} {
		if _, err := parsePositiveOptionalHundredths(input); err == nil {
			t.Fatalf("%q deveria ser inválido para campo opcional positivo", input)
		}
	}
}

func TestHealth(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	app.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil || body["status"] != "ok" {
		t.Fatalf("resposta inesperada: %#v, erro=%v", body, err)
	}
}

func TestPublicRoutes(t *testing.T) {
	app := newTestApp(t)
	routes := []string{"/", "/entrar", "/cadastro", "/recuperar-senha", "/aprenda-4rs"}
	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
			}
		})
	}
}

func TestPrivateRouteRedirectsAnonymousUser(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/perfil", nil))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/entrar" {
		t.Fatalf("status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestPrivateRouteUsesValidatedSession(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/perfil?userId=firebase-user-b", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
	}
	if _, ok := app.profiles.profiles["firebase-user-a"]; !ok {
		t.Fatal("perfil do UID autenticado não foi usado")
	}
	if _, ok := app.profiles.profiles["firebase-user-b"]; ok {
		t.Fatal("userId enviado pelo cliente foi usado")
	}
}

func TestGamificationPagesRequireSessionAndRenderAuthenticatedData(t *testing.T) {
	app := newTestApp(t)
	for _, path := range []string{"/progresso", "/recompensas"} {
		anonymous := httptest.NewRecorder()
		app.handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, path, nil))
		if anonymous.Code != http.StatusSeeOther {
			t.Fatalf("%s anônimo status=%d", path, anonymous.Code)
		}
		authenticated := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
		app.handler.ServeHTTP(authenticated, request)
		if authenticated.Code != http.StatusOK || !strings.Contains(authenticated.Body.String(), "0") {
			t.Fatalf("%s autenticado status=%d corpo=%s", path, authenticated.Code, authenticated.Body.String())
		}
	}
}

func TestProgressPresentationRoundsHalfUp(t *testing.T) {
	tests := []struct {
		name string
		rate progress.Rate
		want int
	}{
		{name: "exato", rate: progress.Rate{Contribution: big.NewRat(1, 1), Denominator: 2}, want: 50},
		{name: "metade para cima", rate: progress.Rate{Contribution: big.NewRat(1, 200), Denominator: 1}, want: 1},
		{name: "abaixo da metade", rate: progress.Rate{Contribution: big.NewRat(49, 10000), Denominator: 1}, want: 0},
		{name: "sem denominador", rate: progress.EmptyRate(), want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ratePercent(test.rate); got != test.want {
				t.Fatalf("ratePercent() = %d, esperado %d", got, test.want)
			}
		})
	}
}

func TestProgressRejectsCustomPeriodLongerThan366Days(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/progresso?periodo=custom&inicio=2024-01-01&fim=2025-01-01", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "no máximo 366 dias") {
		t.Fatalf("status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRankingTopIsVisibleWithoutOptInAndPrivateFieldsStayAbsent(t *testing.T) {
	app := newTestApp(t)
	app.ranking.top = []ranking.Entry{{UID: "ranked-uid", Nickname: "Luna", AvatarType: profile.AvatarPurple, TotalPoints: 2430}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/recompensas", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "Luna") || !strings.Contains(body, "ranking-avatar--purple") || !strings.Contains(body, "ative a participação no Perfil") {
		t.Fatalf("status=%d corpo=%s", recorder.Code, body)
	}
	for _, private := range []string{"ranked-uid", "a@example.com", "America/Sao_Paulo", "weightHundredths"} {
		if strings.Contains(body, private) {
			t.Fatalf("ranking expôs dado privado %q", private)
		}
	}
}

func TestRankingShowsParticipantOutsideTopAndDistance(t *testing.T) {
	app := newTestApp(t)
	now := time.Now().UTC()
	app.profiles.profiles["firebase-user-a"] = profile.Profile{UID: "firebase-user-a", Email: "a@example.com", Nickname: "Nico", Age: 15, Timezone: "UTC", RankingOptIn: true, ProfileComplete: true, TotalPoints: 100, CreatedAt: now, UpdatedAt: now}
	for index := range 10 {
		app.ranking.top = append(app.ranking.top, ranking.Entry{UID: fmt.Sprintf("top-%02d", index), Nickname: fmt.Sprintf("Pessoa %d", index+1), TotalPoints: int64(200 - index)})
	}
	previous := ranking.Entry{UID: "previous", Nickname: "Anterior", TotalPoints: 105}
	app.ranking.self = ranking.Entry{UID: "firebase-user-a", Nickname: "Nico", TotalPoints: 100}
	app.ranking.count = 10
	app.ranking.previous = &previous
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/recompensas", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "11º lugar") || !strings.Contains(body, "Faltam 6 pontos") {
		t.Fatalf("status=%d corpo=%s", recorder.Code, body)
	}
}

func TestSessionCreationRequiresCSRFAndDoesNotCreateProfile(t *testing.T) {
	app := newTestApp(t)

	withoutCSRF := httptest.NewRecorder()
	app.handler.ServeHTTP(withoutCSRF, httptest.NewRequest(http.MethodPost, "/api/auth/session", strings.NewReader(`{"idToken":"token"}`)))
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("sem CSRF: status = %d", withoutCSRF.Code)
	}

	token, cookie := issueCSRF(t, app.handler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/session", strings.NewReader(`{"idToken":"firebase-id-token"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(cookie)
	app.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, corpo=%s", recorder.Code, recorder.Body.String())
	}
	if app.sessions.createdDuration != auth.SessionDuration || app.sessions.createdIDToken != "firebase-id-token" {
		t.Fatalf("sessão criada com token=%q duração=%s", app.sessions.createdIDToken, app.sessions.createdDuration)
	}
	if app.profiles.ensureCalls != 0 {
		t.Fatalf("endpoint de sessão chamou EnsureProfile %d vez(es)", app.profiles.ensureCalls)
	}

	var sessionCookie *http.Cookie
	for _, value := range recorder.Result().Cookies() {
		if value.Name == auth.SessionCookieName {
			sessionCookie = value
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.Path != "/" || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("atributos do cookie de sessão inválidos: %#v", sessionCookie)
	}
}

func TestEnsureAndUpdateProfileUseAuthenticatedUID(t *testing.T) {
	app := newTestApp(t)
	token, csrfCookie := issueCSRF(t, app.handler)
	sessionCookie := &http.Cookie{Name: auth.SessionCookieName, Value: "valid"}

	ensureRecorder := httptest.NewRecorder()
	ensureRequest := httptest.NewRequest(http.MethodPost, "/api/profile/ensure?userId=firebase-user-b", strings.NewReader(`{"timezone":"America/Sao_Paulo"}`))
	ensureRequest.Header.Set(csrf.HeaderName, token)
	ensureRequest.AddCookie(csrfCookie)
	ensureRequest.AddCookie(sessionCookie)
	app.handler.ServeHTTP(ensureRecorder, ensureRequest)
	if ensureRecorder.Code != http.StatusOK {
		t.Fatalf("EnsureProfile status=%d corpo=%s", ensureRecorder.Code, ensureRecorder.Body.String())
	}

	updateRecorder := httptest.NewRecorder()
	current := app.profiles.profiles["firebase-user-a"]
	current.AvatarType = profile.AvatarOrange
	current.PhotoMediaID = "foto-existente"
	current.PhotoObjectPath = "avatars/firebase-user-a/foto-existente.jpg"
	app.profiles.profiles["firebase-user-a"] = current
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/profile?userId=firebase-user-b", bytes.NewBufferString(`{"nickname":"Pessoa A","age":16,"timezone":"America/Sao_Paulo","rankingOptIn":true,"reminderNotificationEnabled":true,"reminderEmailEnabled":false}`))
	updateRequest.Header.Set(csrf.HeaderName, token)
	updateRequest.AddCookie(csrfCookie)
	updateRequest.AddCookie(sessionCookie)
	app.handler.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("Update status=%d corpo=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	if app.profiles.lastUID != "firebase-user-a" {
		t.Fatalf("UID atualizado = %q", app.profiles.lastUID)
	}
	updated := app.profiles.profiles["firebase-user-a"]
	if updated.AvatarType != profile.AvatarOrange || updated.PhotoMediaID != "foto-existente" || updated.PhotoObjectPath != "avatars/firebase-user-a/foto-existente.jpg" || !updated.ReminderNotificationEnabled || updated.ReminderEmailEnabled {
		t.Fatalf("perfil atualizado = %#v", updated)
	}
}

func TestFirstProfileUpdatePreservesOmittedReminderPreferences(t *testing.T) {
	app := newTestApp(t)
	token, csrfCookie := issueCSRF(t, app.handler)
	sessionCookie := &http.Cookie{Name: auth.SessionCookieName, Value: "valid"}

	ensureRecorder := httptest.NewRecorder()
	ensureRequest := httptest.NewRequest(http.MethodPost, "/api/profile/ensure", strings.NewReader(`{"timezone":"America/Sao_Paulo"}`))
	ensureRequest.Header.Set(csrf.HeaderName, token)
	ensureRequest.AddCookie(csrfCookie)
	ensureRequest.AddCookie(sessionCookie)
	app.handler.ServeHTTP(ensureRecorder, ensureRequest)
	if ensureRecorder.Code != http.StatusOK {
		t.Fatalf("EnsureProfile status=%d corpo=%s", ensureRecorder.Code, ensureRecorder.Body.String())
	}

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader(`{"nickname":"Pessoa A","age":16,"timezone":"America/Sao_Paulo","rankingOptIn":false}`))
	updateRequest.Header.Set(csrf.HeaderName, token)
	updateRequest.AddCookie(csrfCookie)
	updateRequest.AddCookie(sessionCookie)
	app.handler.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("Update status=%d corpo=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	updated := app.profiles.profiles["firebase-user-a"]
	if !updated.ReminderNotificationEnabled || !updated.ReminderEmailEnabled {
		t.Fatalf("primeiro salvamento desativou preferências omitidas: %#v", updated)
	}

	explicitFalseRecorder := httptest.NewRecorder()
	explicitFalseRequest := httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader(`{"nickname":"Pessoa A","age":16,"timezone":"America/Sao_Paulo","rankingOptIn":false,"reminderNotificationEnabled":false}`))
	explicitFalseRequest.Header.Set(csrf.HeaderName, token)
	explicitFalseRequest.AddCookie(csrfCookie)
	explicitFalseRequest.AddCookie(sessionCookie)
	app.handler.ServeHTTP(explicitFalseRecorder, explicitFalseRequest)
	if explicitFalseRecorder.Code != http.StatusOK {
		t.Fatalf("Update explícito status=%d corpo=%s", explicitFalseRecorder.Code, explicitFalseRecorder.Body.String())
	}
	updated = app.profiles.profiles["firebase-user-a"]
	if updated.ReminderNotificationEnabled || !updated.ReminderEmailEnabled {
		t.Fatalf("false explícito não foi respeitado ou campo omitido mudou: %#v", updated)
	}
}

func TestPushSubscriptionEndpointsUseAuthenticatedUIDAndDoNotExposeSecrets(t *testing.T) {
	app := newTestApp(t)
	token, csrfCookie := issueCSRF(t, app.handler)
	session := &http.Cookie{Name: auth.SessionCookieName, Value: "valid"}
	create := httptest.NewRequest(http.MethodPost, "/api/reminders/subscriptions", strings.NewReader(`{"endpoint":"https://push.example.test/device","keys":{"p256dh":"private-p256dh","auth":"private-auth"}}`))
	create.Header.Set(csrf.HeaderName, token)
	create.AddCookie(csrfCookie)
	create.AddCookie(session)
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, create)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("criar subscription: status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "push.example") || strings.Contains(recorder.Body.String(), "private-") {
		t.Fatalf("resposta expôs endpoint/chaves: %s", recorder.Body.String())
	}
	if len(app.reminders.subscriptions) != 1 || app.reminders.subscriptions[0].OwnerUID != "firebase-user-a" {
		t.Fatalf("subscription=%#v", app.reminders.subscriptions)
	}
	internal := httptest.NewRequest(http.MethodPost, "/internal/reminders/process", nil)
	internal.AddCookie(session)
	internalRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(internalRecorder, internal)
	if internalRecorder.Code != http.StatusNotFound {
		t.Fatalf("rota interna apareceu na implantação pública: %d", internalRecorder.Code)
	}
}

func TestPrivatePhotoUploadReadAndOwnerIsolation(t *testing.T) {
	app := newTestApp(t)
	identity := app.sessions.identity
	if _, err := profile.NewService(app.profiles).EnsureProfile(context.Background(), identity, "UTC"); err != nil {
		t.Fatal(err)
	}
	token, csrfCookie := issueCSRF(t, app.handler)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("photo", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(part, image.NewRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRequest(http.MethodPost, "/api/profile/photo", &body)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	upload.Header.Set(csrf.HeaderName, token)
	upload.AddCookie(csrfCookie)
	upload.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	uploadRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(uploadRecorder, upload)
	if uploadRecorder.Code != http.StatusOK {
		t.Fatalf("upload status=%d corpo=%s", uploadRecorder.Code, uploadRecorder.Body.String())
	}
	mediaID := app.profiles.profiles[identity.UID].PhotoMediaID
	if mediaID == "" {
		t.Fatal("upload não vinculou mídia ao perfil")
	}
	if strings.Contains(uploadRecorder.Body.String(), mediaID) || strings.Contains(uploadRecorder.Body.String(), "photoMediaId") || strings.Contains(uploadRecorder.Body.String(), "photoObjectPath") || strings.Contains(uploadRecorder.Body.String(), "photoUpdatedAt") {
		t.Fatalf("resposta de upload expôs metadados privados: %s", uploadRecorder.Body.String())
	}
	profilePage := httptest.NewRequest(http.MethodGet, "/perfil", nil)
	profilePage.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	profileRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(profileRecorder, profilePage)
	pageBody := profileRecorder.Body.String()
	if profileRecorder.Code != http.StatusOK || !strings.Contains(pageBody, `/media/avatars/current`) || strings.Contains(pageBody, mediaID) || strings.Contains(pageBody, app.profiles.profiles[identity.UID].PhotoObjectPath) || strings.Contains(pageBody, "photoUpdatedAt") {
		t.Fatalf("HTML expôs metadados privados: status=%d corpo=%s", profileRecorder.Code, pageBody)
	}

	read := httptest.NewRequest(http.MethodGet, "/media/avatars/"+mediaID, nil)
	read.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	readRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(readRecorder, read)
	if readRecorder.Code != http.StatusOK || readRecorder.Header().Get("Cache-Control") != "private, max-age=0, must-revalidate" || readRecorder.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("leitura status=%d headers=%#v", readRecorder.Code, readRecorder.Header())
	}
	conditional := httptest.NewRequest(http.MethodGet, "/media/avatars/current", nil)
	conditional.Header.Set("If-None-Match", readRecorder.Header().Get("ETag"))
	conditional.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	conditionalRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(conditionalRecorder, conditional)
	if conditionalRecorder.Code != http.StatusNotModified {
		t.Fatalf("proprietário com ETag recebeu status %d, esperado 304", conditionalRecorder.Code)
	}

	app.sessions.identity = auth.Identity{UID: "firebase-user-b", Email: "b@example.com"}
	foreign := httptest.NewRequest(http.MethodGet, "/media/avatars/"+mediaID, nil)
	foreign.Header.Set("If-None-Match", readRecorder.Header().Get("ETag"))
	foreign.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	foreignRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(foreignRecorder, foreign)
	if foreignRecorder.Code != http.StatusNotFound {
		t.Fatalf("outro usuário com ETag recebeu status %d, esperado 404 antes de avaliar condição", foreignRecorder.Code)
	}
	anonymous := httptest.NewRequest(http.MethodGet, "/media/avatars/"+mediaID, nil)
	anonymous.Header.Set("If-None-Match", readRecorder.Header().Get("ETag"))
	anonymousRecorder := httptest.NewRecorder()
	app.handler.ServeHTTP(anonymousRecorder, anonymous)
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("anônimo com ETag recebeu status %d, esperado 401", anonymousRecorder.Code)
	}
}

func TestSelectInternalAvatarRemovesPrivatePhoto(t *testing.T) {
	app := newTestApp(t)
	uid := app.sessions.identity.UID
	app.profiles.profiles[uid] = profile.Profile{UID: uid, AvatarType: profile.AvatarBlue, PhotoMediaID: "foto", PhotoObjectPath: "avatars/" + uid + "/foto.jpg"}
	app.avatarRepo.media["foto"] = avatar.Media{ID: "foto", OwnerUID: uid, ObjectPath: "avatars/" + uid + "/foto.jpg"}
	app.avatarStore.values["avatars/"+uid+"/foto.jpg"] = []byte("foto")
	token, csrfCookie := issueCSRF(t, app.handler)
	request := httptest.NewRequest(http.MethodPut, "/api/profile/avatar/internal", strings.NewReader(`{"avatarType":"green"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(csrfCookie)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, request)
	updated := app.profiles.profiles[uid]
	if recorder.Code != http.StatusOK || updated.AvatarType != profile.AvatarGreen || updated.PhotoMediaID != "" {
		t.Fatalf("status=%d perfil=%#v corpo=%s", recorder.Code, updated, recorder.Body.String())
	}
	if _, exists := app.avatarStore.values["avatars/"+uid+"/foto.jpg"]; exists {
		t.Fatal("objeto anterior não foi removido")
	}
}

func TestProfileShowsRealPositionOnlyForOptIn(t *testing.T) {
	app := newTestApp(t)
	now := time.Now().UTC()
	app.profiles.profiles["firebase-user-a"] = profile.Profile{UID: "firebase-user-a", Email: "a@example.com", Nickname: "Nico", Age: 15, AvatarType: profile.AvatarPurple, Timezone: "UTC", RankingOptIn: true, ProfileComplete: true, TotalPoints: 125, CreatedAt: now, UpdatedAt: now}
	app.ranking.self = ranking.Entry{UID: "firebase-user-a", Nickname: "Nico", AvatarType: profile.AvatarPurple, TotalPoints: 125}
	app.ranking.count = 8

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/perfil", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "125 pontos") || !strings.Contains(body, "9º no ranking geral") || !strings.Contains(body, "profile-avatar--purple") {
		t.Fatalf("status=%d corpo=%s", recorder.Code, body)
	}

	value := app.profiles.profiles["firebase-user-a"]
	value.RankingOptIn = false
	app.profiles.profiles["firebase-user-a"] = value
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/perfil", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	app.handler.ServeHTTP(recorder, request)
	if strings.Contains(recorder.Body.String(), "9º no ranking geral") {
		t.Fatal("posição exibida para usuário sem opt-in")
	}
}

func TestAccountDeletionStartUsesLiteralConfirmationAndRecentToken(t *testing.T) {
	app := newTestApp(t)
	app.deletion.startResult = accountdeletion.Result{Stage: string(accountdeletion.StageUniqueness)}
	token, csrfCookie := issueCSRF(t, app.handler)
	request := httptest.NewRequest(http.MethodPost, "/api/account/deletion/start", strings.NewReader(`{"confirmation":"EXCLUIR MINHA CONTA","idToken":"recent-token"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(csrfCookie)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
	if app.deletion.confirmation != accountdeletion.ConfirmationPhrase || app.deletion.idToken != "recent-token" || app.deletion.identity.UID != app.sessions.identity.UID {
		t.Fatalf("entrada da exclusão = %#v", app.deletion)
	}
}

func TestAccountDeletionCompletionClearsSessionAndCSRF(t *testing.T) {
	app := newTestApp(t)
	app.deletion.continueResult = accountdeletion.Result{Complete: true}
	token, csrfCookie := issueCSRF(t, app.handler)
	request := httptest.NewRequest(http.MethodPost, "/api/account/deletion/continue", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(csrfCookie)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
	cleared := map[string]bool{}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.MaxAge < 0 {
			cleared[cookie.Name] = true
		}
	}
	if !cleared[auth.SessionCookieName] || !cleared[csrf.CookieName] {
		t.Fatalf("cookies removidos = %#v", cleared)
	}
}

func TestDeletingAccountCannotUseNormalRoutesButCanContinue(t *testing.T) {
	app := newTestApp(t)
	app.accountState.deleting = true
	request := httptest.NewRequest(http.MethodPost, "/api/profile/ensure", strings.NewReader(`{"timezone":"UTC"}`))
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("rota funcional status=%d", recorder.Code)
	}

	token, csrfCookie := issueCSRF(t, app.handler)
	request = httptest.NewRequest(http.MethodPost, "/api/account/deletion/continue", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrf.HeaderName, token)
	request.AddCookie(csrfCookie)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "valid"})
	recorder = httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("continuação status=%d corpo=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStaticAssetAndServiceWorker(t *testing.T) {
	app := newTestApp(t)
	for _, path := range []string{"/static/css/app.css", "/static/js/app.js", "/static/js/firebase-client.js", "/static/manifest.webmanifest", "/service-worker.js"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d", recorder.Code)
			}
		})
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	app := newTestApp(t)
	recorder := httptest.NewRecorder()
	app.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nao-existe", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusNotFound)
	}
}

func issueCSRF(t *testing.T, handler http.Handler) (string, *http.Cookie) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/csrf", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("emitir CSRF: status=%d", recorder.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decodificar CSRF: %v", err)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == csrf.CookieName {
			return body["csrfToken"], cookie
		}
	}
	t.Fatal("cookie CSRF ausente")
	return "", nil
}

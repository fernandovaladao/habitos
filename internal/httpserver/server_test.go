package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"habitos/internal/auth"
	"habitos/internal/csrf"
	"habitos/internal/execution"
	"habitos/internal/gamification"
	"habitos/internal/habit"
	"habitos/internal/note"
	"habitos/internal/profile"
	"habitos/internal/progress"
)

type fakeSessionManager struct {
	identity        auth.Identity
	verifyErr       error
	createdDuration time.Duration
	createdIDToken  string
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
	handler    http.Handler
	sessions   *fakeSessionManager
	profiles   *fakeProfileRepository
	habits     *fakeHabitRepository
	executions *fakeExecutionRepository
	notes      *fakeNoteRepository
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
	notes := note.NewService(noteRepo, habitService, executionService)
	handler, err := NewHandler(Config{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		FirebaseWeb: FirebaseWebConfig{APIKey: "public-key", ProjectID: "test-project"},
	}, Dependencies{Sessions: sessions, Profiles: profile.NewService(profiles), Habits: habitService, Executions: executionService, Notes: notes, Progress: progress.NewService(&fakeProgressRepository{})})
	if err != nil {
		t.Fatalf("NewHandler() retornou erro: %v", err)
	}
	return testApp{handler: handler, sessions: sessions, profiles: profiles, habits: habits, executions: executionRepo, notes: noteRepo}
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
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/profile?userId=firebase-user-b", bytes.NewBufferString(`{"nickname":"Pessoa A","age":16,"timezone":"America/Sao_Paulo","rankingOptIn":true}`))
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

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"habitos/internal/auth"
	"habitos/internal/config"
	"habitos/internal/execution"
	"habitos/internal/firebaseadmin"
	"habitos/internal/habit"
	"habitos/internal/note"
	"habitos/internal/profile"
	"habitos/internal/progress"
)

func TestAuthenticationAndProfileWithFirebaseEmulators(t *testing.T) {
	if os.Getenv("RUN_FIREBASE_EMULATOR_TESTS") != "1" {
		t.Skip("teste opt-in: defina RUN_FIREBASE_EMULATOR_TESTS=1 com os emuladores ativos")
	}
	requireLocalEnvironment(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	clients, err := firebaseadmin.New(ctx, config.LocalEmulatorProjectID)
	if err != nil {
		t.Fatalf("inicializar clientes Firebase: %v", err)
	}
	t.Cleanup(func() { _ = clients.Close() })

	email := fmt.Sprintf("e2e-%d@example.test", time.Now().UnixNano())
	idToken, uid := createEmulatorUser(t, ctx, email)
	t.Cleanup(func() { deleteEmulatorUser(t, idToken) })
	t.Cleanup(func() {
		_, _ = clients.Firestore.Collection("users").Doc(uid).Delete(context.Background())
	})

	sessions := auth.NewFirebaseSessionManager(clients.Auth)
	sessionCookie, err := sessions.CreateSession(ctx, idToken, auth.SessionDuration)
	if err != nil {
		t.Fatalf("criar sessão: %v", err)
	}
	identity, err := sessions.VerifySession(ctx, sessionCookie)
	if err != nil {
		t.Fatalf("validar sessão: %v", err)
	}
	if identity.UID != uid || identity.Email != email {
		t.Fatalf("identidade = %#v, esperado UID %q e e-mail %q", identity, uid, email)
	}

	profiles := profile.NewService(profile.NewFirestoreRepository(clients.Firestore))
	first, err := profiles.EnsureProfile(ctx, identity, "America/Sao_Paulo")
	if err != nil {
		t.Fatalf("criar perfil mínimo: %v", err)
	}
	second, err := profiles.EnsureProfile(ctx, identity, "UTC")
	if err != nil {
		t.Fatalf("repetir criação do perfil: %v", err)
	}
	if first != second {
		t.Fatalf("EnsureProfile não foi idempotente: primeiro=%#v segundo=%#v", first, second)
	}
	if first.RankingOptIn || first.ProfileComplete {
		t.Fatalf("perfil mínimo deveria ser privado e incompleto: %#v", first)
	}

	habitRepository := habit.NewFirestoreRepository(clients.Firestore)
	habits := habit.NewService(habitRepository)
	created, err := habits.Create(ctx, identity, "America/Sao_Paulo", habit.Input{
		Title: "Ler", Description: "Ler diariamente", GoalType: habit.GoalQuantitative,
		TargetHundredths: 2000, Unit: habit.UnitPages,
		Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "19:00", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification},
	})
	if err != nil {
		t.Fatalf("criar hábito: %v", err)
	}
	t.Cleanup(func() { cleanupEmulatorHabits(t, clients, uid) })
	if _, err := habits.Get(ctx, auth.Identity{UID: "outro-uid", Email: "outro@example.test"}, created.ID); !errors.Is(err, habit.ErrNotFound) {
		t.Fatalf("outro usuário acessou hábito: %v", err)
	}
	if _, err := habits.Update(ctx, auth.Identity{UID: "outro-uid", Email: "outro@example.test"}, "America/Sao_Paulo", created.ID, habit.Input{Title: "Inválido", Description: "Não deve alterar", GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1}, Time: "08:00", WeeklyTargetExecutions: 1, Reminder: habit.ReminderEmail}}); !errors.Is(err, habit.ErrNotFound) {
		t.Fatalf("outro usuário alterou hábito: %v", err)
	}
	forged := created
	forged.OwnerUID = "outro-uid"
	forged.Title = "Alteração indevida"
	if err := habitRepository.Update(ctx, forged, nil); !errors.Is(err, habit.ErrNotFound) {
		t.Fatalf("repositório não repetiu autorização na mutação: %v", err)
	}
	updatedInput := habit.Input{Title: "Ler", Description: "Ler todos os dias", GoalType: habit.GoalQuantitative, TargetHundredths: 2500, Unit: habit.UnitPages, Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5}, Time: "20:00", WeeklyTargetExecutions: 5, Reminder: habit.ReminderBoth}}
	updated, err := habits.Update(ctx, identity, "America/Sao_Paulo", created.ID, updatedInput)
	if err != nil {
		t.Fatalf("atualizar hábito: %v", err)
	}
	if !updated.ScheduleEffectiveAt.After(created.ScheduleEffectiveAt) {
		t.Fatalf("nova agenda não ficou futura: criação=%v edição=%v", created.ScheduleEffectiveAt, updated.ScheduleEffectiveAt)
	}
	lastInput := updatedInput
	lastInput.Schedule.Time = "21:30"
	lastInput.Schedule.Reminder = habit.ReminderEmail
	lastUpdated, err := habits.Update(ctx, identity, "America/Sao_Paulo", created.ID, lastInput)
	if err != nil {
		t.Fatalf("substituir agenda pendente: %v", err)
	}
	if lastUpdated.PendingScheduleVersionID != updated.PendingScheduleVersionID {
		t.Fatalf("edição no mesmo dia criou outra versão pendente: primeira=%q última=%q", updated.PendingScheduleVersionID, lastUpdated.PendingScheduleVersionID)
	}
	versions, err := clients.Firestore.Collection("habits").Doc(created.ID).Collection("scheduleVersions").Documents(ctx).GetAll()
	if err != nil || len(versions) != 2 {
		t.Fatalf("snapshots de agenda=%d erro=%v", len(versions), err)
	}
	pendingSnapshot, err := clients.Firestore.Collection("habits").Doc(created.ID).Collection("scheduleVersions").Doc(lastUpdated.PendingScheduleVersionID).Get(ctx)
	if err != nil {
		t.Fatalf("ler versão pendente: %v", err)
	}
	var pending habit.ScheduleVersion
	if err := pendingSnapshot.DataTo(&pending); err != nil || pending.Schedule.Time != "21:30" || pending.Schedule.Reminder != habit.ReminderEmail {
		t.Fatalf("versão pendente não contém última edição: %#v erro=%v", pending, err)
	}
	executionRepository := execution.NewFirestoreRepository(clients.Firestore)
	executions := execution.NewService(executionRepository)
	versionsForSync, err := habitRepository.ListScheduleVersions(ctx, uid, created.ID)
	if err != nil {
		t.Fatalf("listar versões para materialização: %v", err)
	}
	location, _ := time.LoadLocation("America/Sao_Paulo")
	today := time.Now().In(location).Format("2006-01-02")
	var syncGroup sync.WaitGroup
	syncErrors := make(chan error, 4)
	for range 4 {
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			syncErrors <- executions.SyncHabit(ctx, identity, lastUpdated, versionsForSync, "America/Sao_Paulo", today)
		}()
	}
	syncGroup.Wait()
	close(syncErrors)
	for syncErr := range syncErrors {
		if syncErr != nil {
			t.Fatalf("materialização concorrente: %v", syncErr)
		}
	}
	history, err := executions.History(ctx, identity, created.ID, "")
	if err != nil || len(history) != 1 {
		t.Fatalf("histórico=%#v erro=%v", history, err)
	}
	registered, err := executions.RecordQuantitative(ctx, identity, history[0].ID, 1200)
	if err != nil || registered.Status != execution.StatusPartial {
		t.Fatalf("registrar parcial=%#v erro=%v", registered, err)
	}
	var registrationGroup sync.WaitGroup
	registrationErrors := make(chan error, 4)
	for range 4 {
		registrationGroup.Add(1)
		go func() {
			defer registrationGroup.Done()
			_, registerErr := executions.RecordQuantitative(ctx, identity, history[0].ID, 1200)
			registrationErrors <- registerErr
		}()
	}
	registrationGroup.Wait()
	close(registrationErrors)
	for registerErr := range registrationErrors {
		if registerErr != nil {
			t.Fatalf("registro concorrente: %v", registerErr)
		}
	}
	afterSame, err := executions.Get(ctx, identity, history[0].ID)
	if err != nil || !afterSame.UpdatedAt.Equal(registered.UpdatedAt) {
		t.Fatalf("registro idêntico alterou UpdatedAt: antes=%v depois=%v erro=%v", registered.UpdatedAt, afterSame.UpdatedAt, err)
	}
	completedExecution, err := executions.RecordQuantitative(ctx, identity, history[0].ID, 2000)
	if err != nil || completedExecution.PerformedAt == nil || registered.PerformedAt == nil || !completedExecution.PerformedAt.Equal(*registered.PerformedAt) {
		t.Fatalf("correção parcial-concluído não preservou PerformedAt: %#v erro=%v", completedExecution, err)
	}
	notDoneExecution, err := executions.RecordQuantitative(ctx, identity, history[0].ID, 0)
	if err != nil || notDoneExecution.PerformedAt != nil {
		t.Fatalf("correção not_done não removeu PerformedAt: %#v erro=%v", notDoneExecution, err)
	}
	positiveAgain, err := executions.RecordQuantitative(ctx, identity, history[0].ID, 500)
	if err != nil || positiveAgain.PerformedAt == nil {
		t.Fatalf("retorno positivo não criou PerformedAt: %#v erro=%v", positiveAgain, err)
	}
	if _, err := executions.Get(ctx, auth.Identity{UID: "outro-uid", Email: "outro@test"}, history[0].ID); !errors.Is(err, execution.ErrNotFound) {
		t.Fatalf("outro usuário leu execução: %v", err)
	}
	gamificationHabit, err := habits.Create(ctx, identity, "America/Sao_Paulo", habit.Input{
		Title: "Sequência local", Description: "Validar gamificação", GoalType: habit.GoalSimple,
		Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "08:00", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification},
	})
	if err != nil {
		t.Fatalf("criar hábito de gamificação: %v", err)
	}
	deadline := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Microsecond)
	var streakExecutions []execution.Execution
	for index := 2; index >= 0; index-- {
		date := time.Now().In(location).AddDate(0, 0, -index).Format("2006-01-02")
		candidate := execution.Execution{ID: executionRepository.NewID(), OwnerUID: uid, HabitID: gamificationHabit.ID, ScheduledDate: date, GoalTypeSnapshot: habit.GoalSimple, Status: execution.StatusPending, RegistrationDeadline: deadline, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		ensured, ensureErr := executionRepository.Ensure(ctx, candidate, fmt.Sprintf("gamification-%s-%s", gamificationHabit.ID, date))
		if ensureErr != nil {
			t.Fatalf("materializar sequência: %v", ensureErr)
		}
		completed, recordErr := executions.RecordSimple(ctx, identity, ensured.ID, true)
		if recordErr != nil || completed.PointsAwarded != 10 {
			t.Fatalf("pontuar execução: %#v erro=%v", completed, recordErr)
		}
		streakExecutions = append(streakExecutions, completed)
	}
	streakState, err := executions.Streak(ctx, identity, gamificationHabit.ID)
	if err != nil || streakState.CurrentStreak != 3 || streakState.BestStreak != 3 || len(streakState.MilestonesAwarded) != 1 {
		t.Fatalf("sequência 3 inválida: %#v erro=%v", streakState, err)
	}
	profileAfterBonus, err := profiles.Get(ctx, identity)
	if err != nil || profileAfterBonus.TotalPoints < 40 {
		t.Fatalf("total não recebeu pontos e bônus: %#v erro=%v", profileAfterBonus, err)
	}
	reachedAt := profileAfterBonus.TotalPointsReachedAt
	identical, err := executions.RecordSimple(ctx, identity, streakExecutions[2].ID, true)
	profileAfterIdentical, _ := profiles.Get(ctx, identity)
	if err != nil || !identical.UpdatedAt.Equal(streakExecutions[2].UpdatedAt) || reachedAt == nil || profileAfterIdentical.TotalPointsReachedAt == nil || !profileAfterIdentical.TotalPointsReachedAt.Equal(*reachedAt) {
		t.Fatalf("reenvio idêntico não foi no-op: execução=%#v perfil=%#v erro=%v", identical, profileAfterIdentical, err)
	}
	if _, err := executions.RecordSimple(ctx, identity, streakExecutions[1].ID, false); err != nil {
		t.Fatalf("correção retroativa: %v", err)
	}
	reducedStreak, _ := executions.Streak(ctx, identity, gamificationHabit.ID)
	reducedProfile, _ := profiles.Get(ctx, identity)
	if reducedStreak.CurrentStreak != 1 || reducedStreak.BestStreak != 3 || reducedProfile.TotalPoints != profileAfterBonus.TotalPoints-10 {
		t.Fatalf("correção não preservou histórico: streak=%#v perfil=%#v", reducedStreak, reducedProfile)
	}
	if _, err := executions.RecordSimple(ctx, identity, streakExecutions[1].ID, true); err != nil {
		t.Fatalf("reconstruir sequência: %v", err)
	}
	awards, err := clients.Firestore.Collection("habitBonusAwards").Where("habitId", "==", gamificationHabit.ID).Documents(ctx).GetAll()
	if err != nil || len(awards) != 1 {
		t.Fatalf("bônus histórico duplicado: %d erro=%v", len(awards), err)
	}
	achievements, err := executions.Achievements(ctx, identity)
	if err != nil || len(achievements) != 1 || achievements[0].Name != "Primeira sequência" {
		t.Fatalf("conquista inválida: %#v erro=%v", achievements, err)
	}
	beforeConcurrent, _ := profiles.Get(ctx, identity)
	var correctionGroup sync.WaitGroup
	correctionErrors := make(chan error, 2)
	for _, completed := range []bool{false, true} {
		completed := completed
		correctionGroup.Add(1)
		go func() {
			defer correctionGroup.Done()
			_, correctionErr := executions.RecordSimple(ctx, identity, streakExecutions[1].ID, completed)
			correctionErrors <- correctionErr
		}()
	}
	correctionGroup.Wait()
	close(correctionErrors)
	for correctionErr := range correctionErrors {
		if correctionErr != nil {
			t.Fatalf("correção concorrente: %v", correctionErr)
		}
	}
	finalMiddle, _ := executions.Get(ctx, identity, streakExecutions[1].ID)
	finalProfile, _ := profiles.Get(ctx, identity)
	finalStreak, _ := executions.Streak(ctx, identity, gamificationHabit.ID)
	if finalMiddle.Status == execution.StatusCompleted {
		if finalProfile.TotalPoints != beforeConcurrent.TotalPoints || finalStreak.CurrentStreak != 3 {
			t.Fatalf("estado concorrente concluído inconsistente: execução=%#v perfil=%#v streak=%#v", finalMiddle, finalProfile, finalStreak)
		}
	} else if finalMiddle.Status == execution.StatusNotDone {
		if finalProfile.TotalPoints != beforeConcurrent.TotalPoints-10 || finalStreak.CurrentStreak != 1 {
			t.Fatalf("estado concorrente não realizado inconsistente: execução=%#v perfil=%#v streak=%#v", finalMiddle, finalProfile, finalStreak)
		}
	} else {
		t.Fatalf("estado concorrente inesperado: %#v", finalMiddle)
	}
	if _, err := habits.Archive(ctx, identity, gamificationHabit.ID); err != nil {
		t.Fatalf("arquivar hábito com streak: %v", err)
	}
	if _, err := habits.Reactivate(ctx, identity, "America/Sao_Paulo", gamificationHabit.ID); err != nil {
		t.Fatalf("reativar hábito com streak: %v", err)
	}
	afterReactivation, err := executions.Streak(ctx, identity, gamificationHabit.ID)
	if err != nil || afterReactivation.CurrentStreak != 0 || afterReactivation.BestStreak != 3 || len(afterReactivation.MilestonesAwarded) != 1 {
		t.Fatalf("reativação não preservou histórico: %#v erro=%v", afterReactivation, err)
	}
	legacyHabit, err := habits.Create(ctx, identity, "America/Sao_Paulo", habit.Input{
		Title: "Legado local", Description: "Validar reconciliação", GoalType: habit.GoalSimple,
		Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "07:00", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification},
	})
	if err != nil {
		t.Fatalf("criar hábito legado: %v", err)
	}
	legacyCreatedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	legacyPerformedAt := legacyCreatedAt.Add(10 * time.Minute)
	legacyExecution := execution.Execution{
		ID: executionRepository.NewID(), OwnerUID: uid, HabitID: legacyHabit.ID,
		ScheduledDate: time.Now().In(location).Format("2006-01-02"), GoalTypeSnapshot: habit.GoalSimple,
		Status: execution.StatusCompleted, PerformedAt: &legacyPerformedAt, RegistrationDeadline: deadline,
		CreatedAt: legacyCreatedAt, UpdatedAt: legacyCreatedAt,
	}
	if _, err := clients.Firestore.Collection("executions").Doc(legacyExecution.ID).Create(ctx, legacyExecution); err != nil {
		t.Fatalf("persistir execução pré-Fase 5: %v", err)
	}
	profileBeforeLegacy, _ := profiles.Get(ctx, identity)
	reconciledLegacy, err := executions.RecordSimple(ctx, identity, legacyExecution.ID, true)
	if err != nil || reconciledLegacy.ScoredAt == nil || reconciledLegacy.PointsAwarded != 10 || reconciledLegacy.StreakAfter != 1 || !reconciledLegacy.UpdatedAt.Equal(legacyCreatedAt) {
		t.Fatalf("resultado legado não reconciliado: %#v erro=%v", reconciledLegacy, err)
	}
	profileAfterLegacy, _ := profiles.Get(ctx, identity)
	if profileAfterLegacy.TotalPoints != profileBeforeLegacy.TotalPoints+10 || profileAfterLegacy.TotalPointsReachedAt == nil {
		t.Fatalf("total legado não reconciliado: antes=%#v depois=%#v", profileBeforeLegacy, profileAfterLegacy)
	}
	firstScoredAt := *reconciledLegacy.ScoredAt
	firstTotalReachedAt := *profileAfterLegacy.TotalPointsReachedAt
	legacyRepeated, err := executions.RecordSimple(ctx, identity, legacyExecution.ID, true)
	profileAfterLegacyRepeat, _ := profiles.Get(ctx, identity)
	if err != nil || legacyRepeated.ScoredAt == nil || !legacyRepeated.ScoredAt.Equal(firstScoredAt) || !legacyRepeated.UpdatedAt.Equal(legacyCreatedAt) || profileAfterLegacyRepeat.TotalPoints != profileAfterLegacy.TotalPoints || profileAfterLegacyRepeat.TotalPointsReachedAt == nil || !profileAfterLegacyRepeat.TotalPointsReachedAt.Equal(firstTotalReachedAt) {
		t.Fatalf("segunda repetição legado não foi no-op: execução=%#v perfil=%#v erro=%v", legacyRepeated, profileAfterLegacyRepeat, err)
	}
	notes := note.NewService(note.NewFirestoreRepository(clients.Firestore), habits, executions)
	createdNote, err := notes.Create(ctx, identity, created.ID, history[0].ID, "Reflexão local")
	if err != nil {
		t.Fatalf("criar nota: %v", err)
	}
	if _, err := notes.Update(ctx, auth.Identity{UID: "outro-uid", Email: "outro@test"}, createdNote.ID, "invadir"); !errors.Is(err, note.ErrNotFound) {
		t.Fatalf("outro usuário editou nota: %v", err)
	}
	if _, err := notes.Update(ctx, identity, createdNote.ID, "Reflexão editada"); err != nil {
		t.Fatalf("editar nota: %v", err)
	}
	if err := notes.Delete(ctx, identity, createdNote.ID); err != nil {
		t.Fatalf("excluir nota: %v", err)
	}
	archived, err := habits.Archive(ctx, identity, created.ID)
	if err != nil || archived.Status != habit.StatusArchived {
		t.Fatalf("arquivar=%#v erro=%v", archived, err)
	}
	if _, err := habits.Reactivate(ctx, identity, "America/Sao_Paulo", created.ID); err != nil {
		t.Fatalf("reativar: %v", err)
	}
	duplicated, err := habits.Duplicate(ctx, identity, "America/Sao_Paulo", created.ID)
	if err != nil || duplicated.ID == created.ID {
		t.Fatalf("duplicar=%#v erro=%v", duplicated, err)
	}
	if duplicated.Schedule.Time != "21:30" || duplicated.PendingScheduleVersionID != "" || duplicated.PreviousSchedule != nil {
		t.Fatalf("duplicação não usou a última agenda como inicial: %#v", duplicated)
	}
	if err := habits.Delete(ctx, identity, created.ID); err != nil {
		t.Fatalf("excluir: %v", err)
	}
	if _, err := habits.Get(ctx, identity, created.ID); !errors.Is(err, habit.ErrNotFound) {
		t.Fatalf("hábito excluído permaneceu acessível: %v", err)
	}
	progressService := progress.NewService(progress.NewFirestoreRepository(clients.Firestore))
	progressReport, err := progressService.Report(ctx, identity, "America/Sao_Paulo", progress.Query{Kind: progress.PeriodCustom, StartDate: today, EndDate: today})
	if err != nil {
		t.Fatalf("consultar progresso no Emulator: %v", err)
	}
	if progressReport.Rate.Denominator == 0 || progressReport.Points == 0 || len(progressReport.Achievements) == 0 {
		t.Fatalf("progresso não agregou fatos reais: %#v", progressReport)
	}
	foundMaskedDeleted := false
	for _, item := range progressReport.ByHabit {
		if item.HabitID == created.ID {
			foundMaskedDeleted = item.Deleted && item.Title == "Hábito excluído"
		}
	}
	if !foundMaskedDeleted {
		t.Fatalf("hábito excluído não foi mascarado no detalhamento: %#v", progressReport.ByHabit)
	}
	otherReport, err := progressService.Report(ctx, auth.Identity{UID: "outro-uid"}, "America/Sao_Paulo", progress.Query{Kind: progress.PeriodCustom, StartDate: today, EndDate: today})
	if err != nil || otherReport.Rate.Denominator != 0 || otherReport.Points != 0 {
		t.Fatalf("progresso não isolou outro UID: %#v erro=%v", otherReport, err)
	}
	listed, err := habits.List(ctx, identity, "America/Sao_Paulo", habit.FilterAll)
	if err != nil || len(listed) != 3 {
		t.Fatalf("listagem após CRUD=%#v erro=%v", listed, err)
	}
	foundDuplicated := false
	for _, listedHabit := range listed {
		foundDuplicated = foundDuplicated || listedHabit.ID == duplicated.ID
	}
	if !foundDuplicated {
		t.Fatalf("hábito duplicado ausente da listagem: %#v", listed)
	}
}

func cleanupEmulatorHabits(t *testing.T, clients *firebaseadmin.Clients, uid string) {
	t.Helper()
	ctx := context.Background()
	docs, err := clients.Firestore.Collection("habits").Where("ownerUid", "==", uid).Documents(ctx).GetAll()
	if err != nil {
		t.Logf("limpeza de hábitos: %v", err)
		return
	}
	for _, doc := range docs {
		versions, versionErr := doc.Ref.Collection("scheduleVersions").Documents(ctx).GetAll()
		if versionErr == nil {
			for _, version := range versions {
				_, _ = version.Ref.Delete(ctx)
			}
		}
		_, _ = doc.Ref.Delete(ctx)
	}
	for _, collection := range []string{"executions", "notes", "executionUniqueness", "habitOccurrenceCursors", "habitStreaks", "habitBonusAwards", "userAchievements"} {
		items, itemErr := clients.Firestore.Collection(collection).Where("ownerUid", "==", uid).Documents(ctx).GetAll()
		if itemErr == nil {
			for _, item := range items {
				_, _ = item.Ref.Delete(ctx)
			}
		}
	}
}

func requireLocalEnvironment(t *testing.T) {
	t.Helper()
	want := map[string]string{
		"FIREBASE_PROJECT_ID":         config.LocalEmulatorProjectID,
		"GCLOUD_PROJECT":              config.LocalEmulatorProjectID,
		"FIREBASE_AUTH_EMULATOR_HOST": config.LocalAuthEmulatorHost,
		"FIRESTORE_EMULATOR_HOST":     config.LocalFirestoreEmulatorHost,
	}
	for name, expected := range want {
		if value := os.Getenv(name); value != expected {
			t.Fatalf("%s=%q; esperado %q para impedir acesso a recursos reais", name, value, expected)
		}
	}
}

func createEmulatorUser(t *testing.T, ctx context.Context, email string) (string, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"email":             email,
		"password":          "senha-local-123",
		"returnSecureToken": true,
	})
	if err != nil {
		t.Fatalf("codificar cadastro: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:9099/identitytoolkit.googleapis.com/v1/accounts:signUp?key=demo-key", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("criar requisição de cadastro: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("cadastrar no Auth Emulator: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Auth Emulator retornou status %d", response.StatusCode)
	}
	var result struct {
		IDToken string `json:"idToken"`
		LocalID string `json:"localId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decodificar cadastro: %v", err)
	}
	if result.IDToken == "" || result.LocalID == "" {
		t.Fatalf("cadastro sem token ou UID: %#v", result)
	}
	return result.IDToken, result.LocalID
}

func deleteEmulatorUser(t *testing.T, idToken string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"idToken": idToken})
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:9099/identitytoolkit.googleapis.com/v1/accounts:delete?key=demo-key", bytes.NewReader(body))
	if err != nil {
		t.Logf("não foi possível preparar limpeza do Auth Emulator: %v", err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Logf("não foi possível limpar usuário do Auth Emulator: %v", err)
		return
	}
	_ = response.Body.Close()
}

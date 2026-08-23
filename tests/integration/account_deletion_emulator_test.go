package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	firebaseauth "firebase.google.com/go/v4/auth"
	"habitos/internal/accountdeletion"
	"habitos/internal/accountstate"
	"habitos/internal/auth"
	"habitos/internal/avatar"
	"habitos/internal/config"
	"habitos/internal/execution"
	"habitos/internal/firebaseadmin"
	"habitos/internal/habit"
	"habitos/internal/note"
	"habitos/internal/profile"
	"habitos/internal/reminder"
)

func TestIntegralAccountDeletionWithFirebaseEmulators(t *testing.T) {
	if os.Getenv("RUN_FIREBASE_EMULATOR_TESTS") != "1" {
		t.Skip("teste opt-in: defina RUN_FIREBASE_EMULATOR_TESTS=1 com os emuladores ativos")
	}
	requireLocalEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	clients, err := firebaseadmin.New(ctx, config.LocalEmulatorProjectID, config.LocalEmulatorProjectID+".appspot.com")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clients.Close() })

	email := fmt.Sprintf("delete-%d@example.test", time.Now().UnixNano())
	idToken, uid := createEmulatorUser(t, ctx, email)
	identity := auth.Identity{UID: uid, Email: email}
	profiles := profile.NewService(profile.NewFirestoreRepository(clients.Firestore))
	if _, err := profiles.EnsureProfile(ctx, identity, "UTC"); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Update(ctx, identity, profile.Update{Nickname: "Pessoa teste", Age: 1, Timezone: "UTC", RankingOptIn: true, AvatarType: profile.AvatarBlue, ReminderNotificationEnabled: true, ReminderEmailEnabled: true}); err != nil {
		t.Fatal(err)
	}

	habits := habit.NewService(habit.NewFirestoreRepository(clients.Firestore))
	created, err := habits.Create(ctx, identity, "UTC", habit.Input{Title: "Teste", Description: "Será excluído", GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "08:00", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for index := 0; index < 205; index++ {
		_, err := clients.Firestore.Collection("notes").Doc(fmt.Sprintf("delete-%s-%03d", uid, index)).Set(ctx, note.Note{ID: fmt.Sprintf("n-%03d", index), OwnerUID: uid, HabitID: created.ID, Content: "nota", CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
	}
	otherRef := clients.Firestore.Collection("notes").Doc("preserve-other-" + uid)
	if _, err := otherRef.Set(ctx, map[string]any{"ownerUid": "other-user", "content": "preservar"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = otherRef.Delete(context.Background()) })
	if err := clients.Storage.Object("avatars/" + uid + "/orphan.jpg").NewWriter(ctx).Close(); err != nil {
		t.Fatal(err)
	}
	for collection, id := range map[string]string{reminder.CollectionSubscriptions: "subscription", reminder.CollectionSchedules: created.ID, reminder.CollectionDeliveries: "delivery"} {
		if _, err := clients.Firestore.Collection(collection).Doc(id+"-"+uid).Set(ctx, map[string]any{"ownerUid": uid, "habitId": created.ID, "createdAt": now}); err != nil {
			t.Fatal(err)
		}
	}
	dueSchedule := reminder.Schedule{HabitID: created.ID, OwnerUID: uid, NextScheduledDate: now.Format("2006-01-02"), NextScheduledAt: now.Add(-time.Minute), TimezoneSnapshot: "UTC", ScheduleVersionID: "test", Notification: true, UpdatedAt: now}
	if _, err := clients.Firestore.Collection(reminder.CollectionSchedules).Doc(created.ID).Set(ctx, dueSchedule); err != nil {
		t.Fatal(err)
	}
	claimID := reminder.DeliveryID(uid, created.ID, now.Format("2006-01-02"), reminder.ChannelNotification)
	claimDelivery := reminder.Delivery{ID: claimID, OwnerUID: uid, HabitID: created.ID, ScheduledDate: now.Format("2006-01-02"), Channel: reminder.ChannelNotification, ScheduledAt: now, ExpiresAt: now.Add(30 * time.Minute), NextAttemptAt: now, Status: reminder.StatusPending, CreatedAt: now, UpdatedAt: now}
	if _, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(claimID).Set(ctx, claimDelivery); err != nil {
		t.Fatal(err)
	}

	sessions := auth.NewFirebaseSessionManager(clients.Auth)
	repository := accountdeletion.NewFirestoreRepository(clients.Firestore)
	deletion := accountdeletion.NewService(repository, accountdeletion.NewStorage(clients.Storage), sessions, accountdeletion.NewFirebaseAccountStore(clients.Auth))
	if _, err := deletion.Start(ctx, identity, accountdeletion.ConfirmationPhrase, idToken); err != nil {
		t.Fatalf("iniciar exclusão: %v", err)
	}
	if _, err := clients.Firestore.Collection("publicRanking").Doc(uid).Get(ctx); err == nil {
		t.Fatal("ranking não foi removido no início")
	}

	if _, err := profiles.EnsureProfile(ctx, identity, "UTC"); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("EnsureProfile recriou perfil: %v", err)
	}
	if _, err := habits.Create(ctx, identity, "UTC", habit.Input{Title: "Reaparecer", Description: "Não pode", GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1}, Time: "09:00", WeeklyTargetExecutions: 1, Reminder: habit.ReminderEmail}}); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("hábito reapareceu: %v", err)
	}
	executionRepository := execution.NewFirestoreRepository(clients.Firestore)
	if _, err := executionRepository.Ensure(ctx, execution.Execution{ID: executionRepository.NewID(), OwnerUID: uid, HabitID: created.ID}, "post-marker"); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("execução reapareceu: %v", err)
	}
	noteRepository := note.NewFirestoreRepository(clients.Firestore)
	if _, err := noteRepository.Create(ctx, note.Note{ID: noteRepository.NewID(), OwnerUID: uid, HabitID: created.ID, Content: "não", CreatedAt: now, UpdatedAt: now}); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("nota reapareceu: %v", err)
	}
	avatarService := avatar.NewService(avatar.NewFirestoreRepository(clients.Firestore), avatar.NewStorage(clients.Storage))
	var photo bytes.Buffer
	if err := png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	if _, err := avatarService.Upload(ctx, identity, &photo); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("foto tornou-se ativa: %v", err)
	}
	reminders := reminder.NewService(reminder.NewFirestoreRepository(clients.Firestore), &integrationEmailFake{}, &integrationPushFake{})
	if _, err := reminders.RegisterSubscription(ctx, identity, "https://push.example.test/late", "p256dh", "auth"); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("subscription reapareceu: %v", err)
	}
	reminderRepository := reminder.NewFirestoreRepository(clients.Firestore)
	if err := reminderRepository.ReconcileUser(ctx, uid, now, true); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("projeção reapareceu: %v", err)
	}
	if _, err := reminderRepository.Due(ctx, now, 50); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("delivery foi criada após marcador: %v", err)
	}
	if _, acquired, err := reminderRepository.Claim(ctx, claimID, "lease", now); !errors.Is(err, accountstate.ErrDeleting) || acquired {
		t.Fatalf("lease após marcador: acquired=%v err=%v", acquired, err)
	}
	if err := reminderRepository.Retry(ctx, claimID, 1, now.Add(5*time.Minute)); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("retry após marcador: %v", err)
	}
	if err := reminderRepository.DisableSubscription(ctx, uid, "subscription-"+uid, now); !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("alteração de subscription após marcador: %v", err)
	}

	// Repetir a continuação depois de uma resposta hipoteticamente perdida é seguro.
	for attempts := 0; attempts < 100; attempts++ {
		result, err := deletion.Continue(ctx, identity)
		if err != nil {
			t.Fatalf("continuar exclusão: %v", err)
		}
		if result.Complete {
			break
		}
		if attempts == 99 {
			t.Fatal("exclusão não concluiu")
		}
	}
	if _, err := clients.Auth.GetUser(ctx, uid); !firebaseauth.IsUserNotFound(err) {
		t.Fatalf("Auth ainda existe: %v", err)
	}
	if empty, err := repository.FunctionalEmpty(ctx, uid); err != nil || !empty {
		t.Fatalf("dados funcionais remanescentes: empty=%v err=%v", empty, err)
	}
	if empty, err := accountdeletion.NewStorage(clients.Storage).Empty(ctx, "avatars/"+uid+"/"); err != nil || !empty {
		t.Fatalf("Storage remanescente: empty=%v err=%v", empty, err)
	}
	if _, err := otherRef.Get(ctx); err != nil {
		t.Fatalf("dado de outro usuário afetado: %v", err)
	}
	if deleting, err := accountstate.IsDeleting(ctx, clients.Firestore, uid); err != nil || deleting {
		t.Fatalf("marcador final: deleting=%v err=%v", deleting, err)
	}
}

func TestTransactionStartedBeforeDeletionCannotCommit(t *testing.T) {
	if os.Getenv("RUN_FIREBASE_EMULATOR_TESTS") != "1" {
		t.Skip("teste opt-in")
	}
	requireLocalEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clients, err := firebaseadmin.New(ctx, config.LocalEmulatorProjectID, config.LocalEmulatorProjectID+".appspot.com")
	if err != nil {
		t.Fatal(err)
	}
	defer clients.Close()
	uid := fmt.Sprintf("concurrent-delete-%d", time.Now().UnixNano())
	started, release := make(chan struct{}), make(chan struct{})
	errChannel := make(chan error, 1)
	go func() {
		close(started)
		<-release
		_, err := profile.NewFirestoreRepository(clients.Firestore).Ensure(ctx, profile.Profile{UID: uid, Email: "late@example.test", Timezone: "UTC"})
		errChannel <- err
	}()
	<-started
	repository := accountdeletion.NewFirestoreRepository(clients.Firestore)
	if _, err := repository.Begin(ctx, uid, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errChannel; !errors.Is(err, accountstate.ErrDeleting) {
		t.Fatalf("transação concorrente = %v", err)
	}
	if _, err := clients.Firestore.Collection("users").Doc(uid).Get(ctx); err == nil {
		t.Fatal("transação concorrente recriou perfil")
	}
	_ = repository.RemoveMarker(ctx, uid)
}

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"habitos/internal/auth"
	"habitos/internal/config"
	"habitos/internal/execution"
	"habitos/internal/firebaseadmin"
	"habitos/internal/habit"
	"habitos/internal/profile"
	"habitos/internal/reminder"
)

type integrationEmailFake struct {
	mu    sync.Mutex
	calls []reminder.EmailMessage
}

func (f *integrationEmailFake) SendReminder(_ context.Context, value reminder.EmailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, value)
	return nil
}

type integrationPushFake struct {
	mu    sync.Mutex
	calls []string
}

func (f *integrationPushFake) Send(_ context.Context, value reminder.Subscription, _ reminder.PushMessage) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, value.ID)
	return false, nil
}

func TestRemindersWithFirestoreEmulator(t *testing.T) {
	if os.Getenv("RUN_FIREBASE_EMULATOR_TESTS") != "1" {
		t.Skip("teste opt-in: defina RUN_FIREBASE_EMULATOR_TESTS=1 com os emuladores ativos")
	}
	requireLocalEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	clients, err := firebaseadmin.New(ctx, config.LocalEmulatorProjectID, config.LocalEmulatorProjectID+".appspot.com")
	if err != nil {
		t.Fatal(err)
	}
	defer clients.Close()
	uid := fmt.Sprintf("reminder-%d", time.Now().UnixNano())
	identity := auth.Identity{UID: uid, Email: uid + "@example.test"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	profiles := profile.NewService(profile.NewFirestoreRepository(clients.Firestore))
	if _, err := profiles.EnsureProfile(ctx, identity, "UTC"); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Update(ctx, identity, profile.Update{Nickname: "Lembrete", Age: 15, Timezone: "UTC", AvatarType: profile.AvatarDefault, ReminderNotificationEnabled: true, ReminderEmailEnabled: true}); err != nil {
		t.Fatal(err)
	}
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	habits := habit.NewService(habit.NewFirestoreRepository(clients.Firestore))
	created, err := habits.Create(ctx, identity, "UTC", habit.Input{Title: "Ler", Description: "Ler um pouco", GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{weekday}, Time: now.Format("15:04"), WeeklyTargetExecutions: 1, Reminder: habit.ReminderNotification}})
	if err != nil {
		t.Fatal(err)
	}
	repository := reminder.NewFirestoreRepository(clients.Firestore)
	email := &integrationEmailFake{}
	push := &integrationPushFake{}
	service := reminder.NewService(repository, email, push)
	defer func() {
		for _, collection := range []string{reminder.CollectionSubscriptions, reminder.CollectionDeliveries, reminder.CollectionSchedules, "users"} {
			docs, _ := clients.Firestore.Collection(collection).Where("ownerUid", "==", uid).Documents(context.Background()).GetAll()
			for _, doc := range docs {
				_, _ = doc.Ref.Delete(context.Background())
			}
		}
		cleanupEmulatorHabits(t, clients, uid)
	}()
	if err := service.Reconcile(ctx, identity, false); err != nil {
		t.Fatal(err)
	}
	scheduleRef := clients.Firestore.Collection(reminder.CollectionSchedules).Doc(created.ID)
	snapshot, err := scheduleRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var schedule reminder.Schedule
	if err := snapshot.DataTo(&schedule); err != nil {
		t.Fatal(err)
	}
	// Torna a projeção imediatamente vencida; o processador ainda revalida hábito e agenda autoritativos.
	schedule.NextScheduledDate = now.Format("2006-01-02")
	schedule.NextScheduledAt = now.Add(-time.Minute)
	schedule.TimezoneSnapshot = "UTC"
	if _, err := scheduleRef.Set(ctx, schedule); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		endpoint := fmt.Sprintf("https://push.example.test/%d", index)
		if _, err := service.RegisterSubscription(ctx, identity, endpoint, "p256dh", "auth"); err != nil {
			t.Fatal(err)
		}
	}
	processed, err := service.Process(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("entregas processadas=%d", processed)
	}
	if len(push.calls) != 2 {
		t.Fatalf("fan-out físico=%v", push.calls)
	}
	deliveryID := reminder.DeliveryID(uid, created.ID, now.Format("2006-01-02"), reminder.ChannelNotification)
	deliverySnapshot, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(deliveryID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var delivery reminder.Delivery
	if err := deliverySnapshot.DataTo(&delivery); err != nil {
		t.Fatal(err)
	}
	if delivery.Status != reminder.StatusSent || len(delivery.Physical) != 2 {
		t.Fatalf("entrega=%#v", delivery)
	}
	executionRef := clients.Firestore.Collection("executions").Doc("reminder-status-" + uid)
	for _, test := range []struct {
		status execution.Status
		send   bool
	}{{execution.StatusPending, true}, {execution.StatusCompleted, false}, {execution.StatusPartial, false}, {execution.StatusNotDone, false}} {
		if _, err := executionRef.Set(ctx, execution.Execution{ID: executionRef.ID, OwnerUID: uid, HabitID: created.ID, ScheduledDate: now.Format("2006-01-02"), Status: test.status, ScheduleVersionID: delivery.ScheduleVersionIDSnapshot, TimezoneSnapshot: "UTC", ScheduleSnapshot: habit.Schedule{Weekdays: []int{weekday}, Time: now.Format("15:04"), WeeklyTargetExecutions: 1, Reminder: habit.ReminderNotification}}); err != nil {
			t.Fatal(err)
		}
		eligibility, err := repository.Revalidate(ctx, delivery, now)
		if err != nil {
			t.Fatal(err)
		}
		if eligibility.Send != test.send {
			t.Fatalf("status %s: send=%v", test.status, eligibility.Send)
		}
	}
	// Ocorrência materializada preserva seu snapshot mesmo se a agenda autoritativa posterior divergir.
	materialized := execution.Execution{ID: executionRef.ID, OwnerUID: uid, HabitID: created.ID, ScheduledDate: now.Format("2006-01-02"), Status: execution.StatusPending, ScheduleVersionID: delivery.ScheduleVersionIDSnapshot, TimezoneSnapshot: "America/Sao_Paulo", ScheduleSnapshot: habit.Schedule{Weekdays: []int{weekday}, Time: "07:45", WeeklyTargetExecutions: 1, Reminder: habit.ReminderNotification}}
	if _, err := executionRef.Set(ctx, materialized); err != nil {
		t.Fatal(err)
	}
	versionRef := clients.Firestore.Collection("habits").Doc(created.ID).Collection("scheduleVersions").Doc(delivery.ScheduleVersionIDSnapshot)
	if _, err := versionRef.Update(ctx, []firestore.Update{{Path: "schedule.reminder", Value: habit.ReminderEmail}}); err != nil {
		t.Fatal(err)
	}
	materializedEligibility, err := repository.Revalidate(ctx, delivery, now)
	if err != nil {
		t.Fatal(err)
	}
	if !materializedEligibility.Send || materializedEligibility.Message == nil || materializedEligibility.Message.ScheduledTime != "07:45" {
		t.Fatalf("snapshot materializado não prevaleceu: %#v", materializedEligibility)
	}
	if _, err := versionRef.Update(ctx, []firestore.Update{{Path: "schedule.reminder", Value: habit.ReminderNotification}}); err != nil {
		t.Fatal(err)
	}
	_, _ = executionRef.Delete(ctx)
	// Reprocessar não reenvia a entrega concluída.
	if _, err := scheduleRef.Set(ctx, schedule); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	if len(push.calls) != 2 {
		t.Fatalf("reenvio deliberado para dispositivo entregue: %v", push.calls)
	}
	retryID := reminder.DeliveryID(uid, created.ID, "2099-01-01", reminder.ChannelEmail)
	retryDelivery := reminder.Delivery{ID: retryID, OwnerUID: uid, HabitID: created.ID, ScheduledDate: "2099-01-01", Channel: reminder.ChannelEmail, ScheduledAt: now, ExpiresAt: now.Add(30 * time.Minute), Status: reminder.StatusPending, NextAttemptAt: now.Add(5 * time.Minute), CreatedAt: now, UpdatedAt: now}
	if _, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(retryID).Set(ctx, retryDelivery); err != nil {
		t.Fatal(err)
	}
	dueRetries, err := repository.Due(ctx, now.Add(5*time.Minute), 50)
	if err != nil {
		t.Fatal(err)
	}
	foundRetry := false
	for _, value := range dueRetries {
		if value.ID == retryID {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Fatal("retry deixou de ser consultado depois que a projeção avançou")
	}
	_, _ = clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(retryID).Delete(ctx)
	for index := 2; index < 10; index++ {
		endpoint := fmt.Sprintf("https://push.example.test/%d", index)
		if _, err := service.RegisterSubscription(ctx, identity, endpoint, "p256dh", "auth"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.RegisterSubscription(ctx, identity, "https://push.example.test/11", "p256dh", "auth"); !errors.Is(err, reminder.ErrSubscriptionLimit) {
		t.Fatalf("11ª subscription: %v", err)
	}
	// Editar horário/versão antes do envio atualiza a mesma delivery lógica, sem duplicá-la.
	editable, err := habits.Create(ctx, identity, "UTC", habit.Input{Title: "Agenda futura", Description: "Sem duplicar", GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "00:00", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileUser(ctx, uid, now, true); err != nil {
		t.Fatal(err)
	}
	editableScheduleSnapshot, err := clients.Firestore.Collection(reminder.CollectionSchedules).Doc(editable.ID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var editableSchedule reminder.Schedule
	if err := editableScheduleSnapshot.DataTo(&editableSchedule); err != nil {
		t.Fatal(err)
	}
	editableID := reminder.DeliveryID(uid, editable.ID, editableSchedule.NextScheduledDate, reminder.ChannelNotification)
	pendingEditable := reminder.Delivery{ID: editableID, OwnerUID: uid, HabitID: editable.ID, ScheduledDate: editableSchedule.NextScheduledDate, Channel: reminder.ChannelNotification, ScheduleVersionIDSnapshot: editableSchedule.ScheduleVersionID, TimezoneSnapshot: "UTC", ScheduledAt: editableSchedule.NextScheduledAt, ExpiresAt: editableSchedule.NextScheduledAt.Add(30 * time.Minute), NextAttemptAt: editableSchedule.NextScheduledAt, Status: reminder.StatusPending, Message: reminder.MessageSnapshot{HabitTitle: editable.Title, ScheduledTime: "00:00", Recipient: identity.Email}, CreatedAt: now, UpdatedAt: now}
	if _, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(editableID).Set(ctx, pendingEditable); err != nil {
		t.Fatal(err)
	}
	if _, err := habits.Update(ctx, identity, "UTC", editable.ID, habit.Input{Title: editable.Title, Description: editable.Description, GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "00:30", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileUser(ctx, uid, now, true); err != nil {
		t.Fatal(err)
	}
	updatedEditableSnapshot, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(editableID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var updatedEditable reminder.Delivery
	if err := updatedEditableSnapshot.DataTo(&updatedEditable); err != nil {
		t.Fatal(err)
	}
	duplicates, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Where("ownerUid", "==", uid).Where("habitId", "==", editable.ID).Where("scheduledDate", "==", editableSchedule.NextScheduledDate).Where("channel", "==", reminder.ChannelNotification).Documents(ctx).GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(duplicates) != 1 || updatedEditable.ID != editableID || updatedEditable.Message.ScheduledTime != "00:30" {
		t.Fatalf("edição futura duplicou/não reconciliou: count=%d delivery=%#v", len(duplicates), updatedEditable)
	}
	// Projeção forjada não autoriza envio quando o hábito está arquivado ou excluído.
	for _, deleted := range []bool{false, true} {
		staleHabit, err := habits.Create(ctx, identity, "UTC", habit.Input{Title: "Stale", Description: "Projeção não autoriza", GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: now.Format("15:04"), WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification}})
		if err != nil {
			t.Fatal(err)
		}
		if deleted {
			if err := habits.Delete(ctx, identity, staleHabit.ID); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := habits.Archive(ctx, identity, staleHabit.ID); err != nil {
				t.Fatal(err)
			}
		}
		staleSchedule := reminder.Schedule{HabitID: staleHabit.ID, OwnerUID: uid, NextScheduledDate: now.Format("2006-01-02"), NextScheduledAt: now.Add(-time.Minute), TimezoneSnapshot: "UTC", ScheduleVersionID: "forged", Notification: true, UpdatedAt: now}
		if _, err := clients.Firestore.Collection(reminder.CollectionSchedules).Doc(staleHabit.ID).Set(ctx, staleSchedule); err != nil {
			t.Fatal(err)
		}
		before := len(push.calls)
		if _, err := service.Process(ctx); err != nil {
			t.Fatal(err)
		}
		if len(push.calls) != before {
			t.Fatalf("projeção stale enviou para hábito inativo deleted=%v", deleted)
		}
	}
	// Uma ocorrência já lembrada não ganha uma segunda entrega ao atravessar a data civil por mudança de timezone.
	equivalentHabit, err := habits.Create(ctx, identity, "UTC", habit.Input{Title: "Virada", Description: "Teste de timezone", GoalType: habit.GoalSimple, Schedule: habit.Schedule{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Time: "00:15", WeeklyTargetExecutions: 7, Reminder: habit.ReminderNotification}})
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 23, 23, 30, 0, 0, time.UTC)
	if err := repository.ReconcileUser(ctx, uid, fixed, false); err != nil {
		t.Fatal(err)
	}
	oldScheduleSnapshot, err := clients.Firestore.Collection(reminder.CollectionSchedules).Doc(equivalentHabit.ID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var oldSchedule reminder.Schedule
	if err := oldScheduleSnapshot.DataTo(&oldSchedule); err != nil {
		t.Fatal(err)
	}
	oldID := reminder.DeliveryID(uid, equivalentHabit.ID, oldSchedule.NextScheduledDate, reminder.ChannelNotification)
	if _, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(oldID).Set(ctx, reminder.Delivery{ID: oldID, OwnerUID: uid, HabitID: equivalentHabit.ID, ScheduledDate: oldSchedule.NextScheduledDate, Channel: reminder.ChannelNotification, ScheduledAt: oldSchedule.NextScheduledAt, ExpiresAt: oldSchedule.NextScheduledAt.Add(30 * time.Minute), NextAttemptAt: oldSchedule.NextScheduledAt, Status: reminder.StatusPending, CreatedAt: fixed, UpdatedAt: fixed}); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Update(ctx, identity, profile.Update{Nickname: "Lembrete", Age: 15, Timezone: "Asia/Tokyo", AvatarType: profile.AvatarDefault, ReminderNotificationEnabled: true, ReminderEmailEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileUser(ctx, uid, fixed, true); err != nil {
		t.Fatal(err)
	}
	newScheduleSnapshot, err := clients.Firestore.Collection(reminder.CollectionSchedules).Doc(equivalentHabit.ID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var newSchedule reminder.Schedule
	if err := newScheduleSnapshot.DataTo(&newSchedule); err != nil {
		t.Fatal(err)
	}
	if newSchedule.NextScheduledDate == oldSchedule.NextScheduledDate {
		t.Fatalf("timezone não atravessou data: %#v %#v", oldSchedule, newSchedule)
	}
	oldPendingSnapshot, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(oldID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var oldPending reminder.Delivery
	if err := oldPendingSnapshot.DataTo(&oldPending); err != nil {
		t.Fatal(err)
	}
	if oldPending.Status != reminder.StatusSkipped {
		t.Fatalf("entrega futura antiga não foi supersedida: %#v", oldPending)
	}
	// Volta ao timezone original, marca a ocorrência equivalente como enviada e atravessa a data novamente.
	if _, err := profiles.Update(ctx, identity, profile.Update{Nickname: "Lembrete", Age: 15, Timezone: "UTC", AvatarType: profile.AvatarDefault, ReminderNotificationEnabled: true, ReminderEmailEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileUser(ctx, uid, fixed, true); err != nil {
		t.Fatal(err)
	}
	sent := oldPending
	sent.Status, sent.FailureCode, sent.SkippedAt, sent.SentAt = reminder.StatusSent, "", nil, &fixed
	if _, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(oldID).Set(ctx, sent); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Update(ctx, identity, profile.Update{Nickname: "Lembrete", Age: 15, Timezone: "Asia/Tokyo", AvatarType: profile.AvatarDefault, ReminderNotificationEnabled: true, ReminderEmailEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileUser(ctx, uid, fixed, true); err != nil {
		t.Fatal(err)
	}
	newID := reminder.DeliveryID(uid, equivalentHabit.ID, newSchedule.NextScheduledDate, reminder.ChannelNotification)
	newDeliverySnapshot, err := clients.Firestore.Collection(reminder.CollectionDeliveries).Doc(newID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var equivalent reminder.Delivery
	if err := newDeliverySnapshot.DataTo(&equivalent); err != nil {
		t.Fatal(err)
	}
	if equivalent.Status != reminder.StatusSkipped || equivalent.EquivalentTo != oldID {
		t.Fatalf("equivalência após timezone=%#v", equivalent)
	}
}

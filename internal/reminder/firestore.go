package reminder

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"habitos/internal/accountstate"
	"habitos/internal/execution"
	"habitos/internal/habit"
	"habitos/internal/profile"
)

type FirestoreRepository struct{ client *firestore.Client }

func NewFirestoreRepository(client *firestore.Client) *FirestoreRepository {
	return &FirestoreRepository{client: client}
}

func (r *FirestoreRepository) ReconcileUser(ctx context.Context, uid string, now time.Time, timezoneChanged bool) error {
	userSnapshot, err := r.client.Collection("users").Doc(uid).Get(ctx)
	if err != nil {
		return fmt.Errorf("carregar perfil para lembretes: %w", err)
	}
	var user profile.Profile
	if err := userSnapshot.DataTo(&user); err != nil {
		return fmt.Errorf("decodificar perfil para lembretes: %w", err)
	}
	habitDocs, err := r.client.Collection("habits").Where("ownerUid", "==", uid).Documents(ctx).GetAll()
	if err != nil {
		return fmt.Errorf("listar hábitos para lembretes: %w", err)
	}
	active := make(map[string]bool, len(habitDocs))
	for _, document := range habitDocs {
		var value habit.Habit
		if err := document.DataTo(&value); err != nil {
			return err
		}
		active[value.ID] = true
		if value.DeletedAt != nil || value.Status != habit.StatusActive {
			if err := r.deleteSchedule(ctx, uid, value.ID); err != nil {
				return err
			}
			continue
		}
		versionDocs, err := document.Ref.Collection("scheduleVersions").Documents(ctx).GetAll()
		if err != nil {
			return err
		}
		versions := make([]habit.ScheduleVersion, 0, len(versionDocs))
		for _, versionDoc := range versionDocs {
			var version habit.ScheduleVersion
			if err := versionDoc.DataTo(&version); err != nil {
				return err
			}
			versions = append(versions, version)
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i].EffectiveDate < versions[j].EffectiveDate })
		candidate, ok := nextSchedule(value, versions, user.Timezone, now)
		if !ok {
			if err := r.deleteSchedule(ctx, uid, value.ID); err != nil {
				return err
			}
			continue
		}
		candidate.OwnerUID, candidate.UpdatedAt = uid, now
		if err := r.upsertSchedule(ctx, candidate, timezoneChanged); err != nil {
			return err
		}
	}
	existing, err := r.client.Collection(CollectionSchedules).Where("ownerUid", "==", uid).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, document := range existing {
		if !active[document.Ref.ID] {
			if err := r.deleteSchedule(ctx, uid, document.Ref.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *FirestoreRepository) upsertSchedule(ctx context.Context, value Schedule, timezoneChanged bool) error {
	ref := r.client.Collection(CollectionSchedules).Doc(value.HabitID)
	return r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
		oldSnapshot, oldErr := tx.Get(ref)
		var old Schedule
		oldDeliveries := make(map[Channel]Delivery)
		if oldErr == nil {
			if err := oldSnapshot.DataTo(&old); err != nil {
				return err
			}
			if old.OwnerUID != value.OwnerUID {
				return habit.ErrNotFound
			}
			value.ReconciliationGeneration = old.ReconciliationGeneration + 1
			for _, channel := range scheduleChannels(old) {
				oldID := DeliveryID(value.OwnerUID, value.HabitID, old.NextScheduledDate, channel)
				snapshot, err := tx.Get(r.client.Collection(CollectionDeliveries).Doc(oldID))
				if err == nil {
					var delivered Delivery
					if err := snapshot.DataTo(&delivered); err != nil {
						return err
					}
					oldDeliveries[channel] = delivered
				} else if status.Code(err) != codes.NotFound {
					return err
				}
			}
		} else if status.Code(oldErr) != codes.NotFound {
			return oldErr
		}
		for channel, delivered := range oldDeliveries {
			newChannelEnabled := channel == ChannelNotification && value.Notification || channel == ChannelEmail && value.Email
			if timezoneChanged && delivered.Status == StatusPending && old.NextScheduledDate == value.NextScheduledDate && newChannelEnabled {
				delivered.ScheduleVersionIDSnapshot, delivered.TimezoneSnapshot, delivered.ScheduledAt, delivered.ExpiresAt, delivered.NextAttemptAt, delivered.UpdatedAt = value.ScheduleVersionID, value.TimezoneSnapshot, value.NextScheduledAt, value.NextScheduledAt.Add(DeliveryLifetime), value.NextScheduledAt, value.UpdatedAt
				delivered.Message.ScheduledTime = value.NextScheduledAt.In(mustLocation(value.TimezoneSnapshot)).Format("15:04")
				if err := tx.Set(r.client.Collection(CollectionDeliveries).Doc(delivered.ID), delivered); err != nil {
					return err
				}
			} else if timezoneChanged && delivered.Status == StatusPending && (old.NextScheduledDate != value.NextScheduledDate || !newChannelEnabled) {
				delivered.Status, delivered.FailureCode, delivered.SkippedAt, delivered.UpdatedAt = StatusSkipped, "schedule_superseded", &value.UpdatedAt, value.UpdatedAt
				if err := tx.Set(r.client.Collection(CollectionDeliveries).Doc(delivered.ID), delivered); err != nil {
					return err
				}
			} else if timezoneChanged && old.NextScheduledDate != value.NextScheduledDate && delivered.Status == StatusSent && newChannelEnabled {
				newID := DeliveryID(value.OwnerUID, value.HabitID, value.NextScheduledDate, channel)
				skipped := Delivery{ID: newID, OwnerUID: value.OwnerUID, HabitID: value.HabitID, ScheduledDate: value.NextScheduledDate, Channel: channel, ScheduledAt: value.NextScheduledAt, Status: StatusSkipped, EquivalentTo: delivered.ID, FailureCode: "timezone_equivalent_already_sent", SkippedAt: &value.UpdatedAt, CreatedAt: value.UpdatedAt, UpdatedAt: value.UpdatedAt}
				if err := tx.Set(r.client.Collection(CollectionDeliveries).Doc(newID), skipped); err != nil {
					return err
				}
			}
		}
		return tx.Set(ref, value)
	})
}

func (r *FirestoreRepository) deleteSchedule(ctx context.Context, uid, habitID string) error {
	ref := r.client.Collection(CollectionSchedules).Doc(habitID)
	return r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, uid); err != nil {
			return err
		}
		snapshot, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var value Schedule
		if err := snapshot.DataTo(&value); err != nil {
			return err
		}
		if value.OwnerUID != uid {
			return habit.ErrNotFound
		}
		return tx.Delete(ref)
	})
}

func nextSchedule(value habit.Habit, versions []habit.ScheduleVersion, timezone string, after time.Time) (Schedule, bool) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Schedule{}, false
	}
	local := after.In(location)
	for offset := 0; offset < 15; offset++ {
		dateValue := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).AddDate(0, 0, offset)
		date := dateValue.Format("2006-01-02")
		if date < value.ScheduleEffectiveDate || date < value.OccurrencesResumeDate {
			continue
		}
		version, ok := versionForDate(versions, date)
		if !ok {
			continue
		}
		weekday := int(dateValue.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		if !containsDay(version.Schedule.Weekdays, weekday) {
			continue
		}
		due, ok := resolveCivilTime(date, version.Schedule.Time, location)
		if !ok || due.Before(after) {
			continue
		}
		return Schedule{HabitID: value.ID, NextScheduledDate: date, NextScheduledAt: due.UTC(), TimezoneSnapshot: timezone, ScheduleVersionID: version.ID, Notification: version.Schedule.Reminder == habit.ReminderNotification || version.Schedule.Reminder == habit.ReminderBoth, Email: version.Schedule.Reminder == habit.ReminderEmail || version.Schedule.Reminder == habit.ReminderBoth}, true
	}
	return Schedule{}, false
}

func versionForDate(versions []habit.ScheduleVersion, date string) (habit.ScheduleVersion, bool) {
	var result habit.ScheduleVersion
	ok := false
	for _, value := range versions {
		if value.EffectiveDate <= date {
			result, ok = value, true
		}
	}
	return result, ok
}
func containsDay(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func scheduleChannels(value Schedule) []Channel {
	var result []Channel
	if value.Notification {
		result = append(result, ChannelNotification)
	}
	if value.Email {
		result = append(result, ChannelEmail)
	}
	return result
}

func resolveCivilTime(date, clock string, location *time.Location) (time.Time, bool) {
	dateParts, dateErr := time.Parse("2006-01-02", date)
	clockParts := strings.Split(clock, ":")
	if dateErr != nil || len(clockParts) != 2 {
		return time.Time{}, false
	}
	hour, errH := strconv.Atoi(clockParts[0])
	minute, errM := strconv.Atoi(clockParts[1])
	if errH != nil || errM != nil {
		return time.Time{}, false
	}
	start := time.Date(dateParts.Year(), dateParts.Month(), dateParts.Day(), 0, 0, 0, 0, location).Add(-2 * time.Hour)
	var firstAfter time.Time
	for instant := start; instant.Before(start.Add(30 * time.Hour)); instant = instant.Add(time.Minute) {
		local := instant.In(location)
		if local.Format("2006-01-02") != date {
			continue
		}
		if local.Hour() == hour && local.Minute() == minute {
			return instant, true
		}
		if firstAfter.IsZero() && (local.Hour() > hour || local.Hour() == hour && local.Minute() > minute) {
			firstAfter = instant
		}
	}
	return firstAfter, !firstAfter.IsZero()
}

func (r *FirestoreRepository) Due(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	schedules, err := r.client.Collection(CollectionSchedules).Where("nextScheduledAt", "<=", now).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	resultByID := make(map[string]Delivery)
	owners := map[string]bool{}
	for _, snapshot := range schedules {
		var schedule Schedule
		if err := snapshot.DataTo(&schedule); err != nil {
			return nil, err
		}
		owners[schedule.OwnerUID] = true
		for _, channel := range scheduleChannels(schedule) {
			delivery, err := r.ensureDelivery(ctx, schedule, channel, now)
			if err != nil {
				return nil, err
			}
			if delivery.Status == StatusPending || delivery.Status == StatusProcessing {
				resultByID[delivery.ID] = delivery
			}
		}
	}
	for uid := range owners {
		if err := r.ReconcileUser(ctx, uid, now.Add(time.Minute), false); err != nil {
			return nil, err
		}
	}
	pending, err := r.client.Collection(CollectionDeliveries).Where("status", "==", StatusPending).Where("nextAttemptAt", "<=", now).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	expiredLeases, err := r.client.Collection(CollectionDeliveries).Where("status", "==", StatusProcessing).Where("leaseUntil", "<=", now).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	for _, snapshot := range append(pending, expiredLeases...) {
		var value Delivery
		if err := snapshot.DataTo(&value); err != nil {
			return nil, err
		}
		resultByID[value.ID] = value
	}
	result := make([]Delivery, 0, len(resultByID))
	for _, value := range resultByID {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NextAttemptAt.Before(result[j].NextAttemptAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *FirestoreRepository) ensureDelivery(ctx context.Context, schedule Schedule, channel Channel, now time.Time) (Delivery, error) {
	id := DeliveryID(schedule.OwnerUID, schedule.HabitID, schedule.NextScheduledDate, channel)
	ref := r.client.Collection(CollectionDeliveries).Doc(id)
	var result Delivery
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, schedule.OwnerUID); err != nil {
			return err
		}
		if snapshot, err := tx.Get(ref); err == nil {
			return snapshot.DataTo(&result)
		} else if status.Code(err) != codes.NotFound {
			return err
		}
		habitSnapshot, err := tx.Get(r.client.Collection("habits").Doc(schedule.HabitID))
		if err != nil {
			return err
		}
		var habitValue habit.Habit
		if err := habitSnapshot.DataTo(&habitValue); err != nil {
			return err
		}
		userSnapshot, err := tx.Get(r.client.Collection("users").Doc(schedule.OwnerUID))
		if err != nil {
			return err
		}
		var user profile.Profile
		if err := userSnapshot.DataTo(&user); err != nil {
			return err
		}
		clock := schedule.NextScheduledAt.In(mustLocation(schedule.TimezoneSnapshot)).Format("15:04")
		result = Delivery{ID: id, OwnerUID: schedule.OwnerUID, HabitID: schedule.HabitID, ScheduledDate: schedule.NextScheduledDate, Channel: channel, ScheduleVersionIDSnapshot: schedule.ScheduleVersionID, TimezoneSnapshot: schedule.TimezoneSnapshot, ScheduledAt: schedule.NextScheduledAt, ExpiresAt: schedule.NextScheduledAt.Add(DeliveryLifetime), Message: MessageSnapshot{HabitTitle: habitValue.Title, ScheduledTime: clock, Recipient: user.Email}, Status: StatusPending, NextAttemptAt: schedule.NextScheduledAt, Physical: map[string]PhysicalDelivery{}, CreatedAt: now, UpdatedAt: now}
		return tx.Create(ref, result)
	})
	return result, err
}

func mustLocation(value string) *time.Location {
	location, err := time.LoadLocation(value)
	if err != nil {
		return time.UTC
	}
	return location
}

func (r *FirestoreRepository) Claim(ctx context.Context, id, leaseID string, now time.Time) (Delivery, bool, error) {
	ref := r.client.Collection(CollectionDeliveries).Doc(id)
	var value Delivery
	acquired := false
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := snapshot.DataTo(&value); err != nil {
			return err
		}
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
		if value.Status == StatusSent || value.Status == StatusSkipped || value.Status == StatusFailed || now.Before(value.NextAttemptAt) || value.LeaseUntil != nil && now.Before(*value.LeaseUntil) {
			return nil
		}
		if !now.Before(value.ExpiresAt) {
			value.Status, value.FailureCode, value.UpdatedAt, value.LeaseID, value.LeaseUntil = StatusFailed, "expired", now, "", nil
			return tx.Set(ref, value)
		}
		until := now.Add(LeaseDuration)
		value.Status, value.LeaseID, value.LeaseUntil, value.UpdatedAt = StatusProcessing, leaseID, &until, now
		acquired = true
		return tx.Set(ref, value)
	})
	return value, acquired, err
}

func (r *FirestoreRepository) Revalidate(ctx context.Context, value Delivery, now time.Time) (Eligibility, error) {
	if deleting, err := accountstate.NewFirestoreChecker(r.client).IsDeleting(ctx, value.OwnerUID); err != nil || deleting {
		return Eligibility{SkipCode: "account_deleting"}, err
	}
	userSnapshot, err := r.client.Collection("users").Doc(value.OwnerUID).Get(ctx)
	if err != nil {
		return Eligibility{SkipCode: "profile_missing"}, nil
	}
	var user profile.Profile
	if err := userSnapshot.DataTo(&user); err != nil {
		return Eligibility{}, err
	}
	habitSnapshot, err := r.client.Collection("habits").Doc(value.HabitID).Get(ctx)
	if err != nil {
		return Eligibility{SkipCode: "habit_missing"}, nil
	}
	var habitValue habit.Habit
	if err := habitSnapshot.DataTo(&habitValue); err != nil {
		return Eligibility{}, err
	}
	if habitValue.OwnerUID != value.OwnerUID || habitValue.DeletedAt != nil || habitValue.Status != habit.StatusActive || value.ScheduledDate < habitValue.OccurrencesResumeDate {
		return Eligibility{SkipCode: "habit_inactive"}, nil
	}
	dateValue, dateErr := time.Parse("2006-01-02", value.ScheduledDate)
	weekday := 0
	if dateErr == nil {
		weekday = int(dateValue.Weekday())
		if weekday == 0 {
			weekday = 7
		}
	}
	executions, err := r.client.Collection("executions").Where("ownerUid", "==", value.OwnerUID).Where("habitId", "==", value.HabitID).Where("scheduledDate", "==", value.ScheduledDate).Limit(1).Documents(ctx).GetAll()
	if err != nil {
		return Eligibility{}, err
	}
	var executionValue *execution.Execution
	if len(executions) > 0 {
		var materialized execution.Execution
		if err := executions[0].DataTo(&materialized); err != nil {
			return Eligibility{}, err
		}
		if materialized.Status != execution.StatusPending {
			return Eligibility{SkipCode: "execution_resolved"}, nil
		}
		executionValue = &materialized
	}
	var applicable habit.Schedule
	if executionValue != nil {
		applicable = executionValue.ScheduleSnapshot
		if dateErr != nil || !containsDay(applicable.Weekdays, weekday) {
			return Eligibility{SkipCode: "execution_snapshot_invalid"}, nil
		}
		message := value.Message
		message.ScheduledTime = applicable.Time
		if value.TimezoneSnapshot != executionValue.TimezoneSnapshot || value.ScheduleVersionIDSnapshot != executionValue.ScheduleVersionID || value.Message.ScheduledTime != message.ScheduledTime {
			err := r.updateDelivery(ctx, value.ID, func(delivery *Delivery) {
				delivery.TimezoneSnapshot, delivery.ScheduleVersionIDSnapshot, delivery.Message, delivery.UpdatedAt = executionValue.TimezoneSnapshot, executionValue.ScheduleVersionID, message, now
			})
			if err != nil {
				return Eligibility{}, err
			}
		}
		value.Message = message
	} else {
		versionDocs, err := habitSnapshot.Ref.Collection("scheduleVersions").Documents(ctx).GetAll()
		if err != nil {
			return Eligibility{}, err
		}
		versions := make([]habit.ScheduleVersion, 0, len(versionDocs))
		for _, document := range versionDocs {
			var version habit.ScheduleVersion
			if err := document.DataTo(&version); err != nil {
				return Eligibility{}, err
			}
			versions = append(versions, version)
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i].EffectiveDate < versions[j].EffectiveDate })
		version, scheduled := versionForDate(versions, value.ScheduledDate)
		if !scheduled || dateErr != nil || !containsDay(version.Schedule.Weekdays, weekday) {
			return Eligibility{SkipCode: "schedule_changed"}, nil
		}
		applicable = version.Schedule
	}
	channelAllowed := value.Channel == ChannelNotification && (applicable.Reminder == habit.ReminderNotification || applicable.Reminder == habit.ReminderBoth) || value.Channel == ChannelEmail && (applicable.Reminder == habit.ReminderEmail || applicable.Reminder == habit.ReminderBoth)
	if !channelAllowed {
		return Eligibility{SkipCode: "schedule_changed"}, nil
	}
	if value.Channel == ChannelEmail {
		if !user.ReminderEmailEnabled {
			return Eligibility{SkipCode: "email_disabled"}, nil
		}
		return Eligibility{Send: true, Message: &value.Message}, nil
	}
	if !user.ReminderNotificationEnabled {
		return Eligibility{SkipCode: "notification_disabled"}, nil
	}
	subscriptions, err := r.ListSubscriptions(ctx, value.OwnerUID)
	if err != nil {
		return Eligibility{}, err
	}
	if len(subscriptions) == 0 {
		return Eligibility{SkipCode: "no_subscription"}, nil
	}
	return Eligibility{Send: true, Subscriptions: subscriptions, Message: &value.Message}, nil
}

func (r *FirestoreRepository) RecordPhysical(ctx context.Context, deliveryID, subscriptionID string, physical PhysicalDelivery, now time.Time) error {
	return r.updateDelivery(ctx, deliveryID, func(value *Delivery) {
		if value.Physical == nil {
			value.Physical = map[string]PhysicalDelivery{}
		}
		value.Physical[subscriptionID] = physical
		value.UpdatedAt = now
	})
}
func (r *FirestoreRepository) Finish(ctx context.Context, id string, target Status, code string, now time.Time) error {
	return r.updateDelivery(ctx, id, func(value *Delivery) {
		value.Status, value.FailureCode, value.UpdatedAt, value.LeaseID, value.LeaseUntil = target, code, now, "", nil
		if target == StatusSent {
			value.SentAt = &now
		} else if target == StatusSkipped {
			value.SkippedAt = &now
		}
	})
}
func (r *FirestoreRepository) Retry(ctx context.Context, id string, attempt int, at time.Time) error {
	return r.updateDelivery(ctx, id, func(value *Delivery) {
		value.Status, value.AttemptCount, value.NextAttemptAt, value.LeaseID, value.LeaseUntil, value.UpdatedAt = StatusPending, attempt, at, "", nil, time.Now().UTC().Truncate(time.Microsecond)
	})
}

func (r *FirestoreRepository) updateDelivery(ctx context.Context, id string, mutate func(*Delivery)) error {
	ref := r.client.Collection(CollectionDeliveries).Doc(id)
	return r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var value Delivery
		if err := snapshot.DataTo(&value); err != nil {
			return err
		}
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
		mutate(&value)
		return tx.Set(ref, value)
	})
}

func (r *FirestoreRepository) UpsertSubscription(ctx context.Context, value Subscription) (Subscription, error) {
	active, err := r.ListSubscriptions(ctx, value.OwnerUID)
	if err != nil {
		return Subscription{}, err
	}
	found := false
	for _, item := range active {
		if item.ID == value.ID {
			value.CreatedAt = item.CreatedAt
			found = true
		}
	}
	if !found && len(active) >= MaxActiveSubscriptions {
		return Subscription{}, ErrSubscriptionLimit
	}
	ref := r.client.Collection(CollectionSubscriptions).Doc(value.ID)
	err = r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, value.OwnerUID); err != nil {
			return err
		}
		return tx.Set(ref, value)
	})
	return value, err
}
func (r *FirestoreRepository) DisableSubscription(ctx context.Context, uid, id string, now time.Time) error {
	ref := r.client.Collection(CollectionSubscriptions).Doc(id)
	return r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := accountstate.AssertActiveTransaction(tx, r.client, uid); err != nil {
			return err
		}
		snapshot, err := tx.Get(ref)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var value Subscription
		if err := snapshot.DataTo(&value); err != nil {
			return err
		}
		if value.OwnerUID != uid {
			return ErrNotFound
		}
		value.DisabledAt, value.UpdatedAt = &now, now
		return tx.Set(ref, value)
	})
}
func (r *FirestoreRepository) ListSubscriptions(ctx context.Context, uid string) ([]Subscription, error) {
	iteratorDocs := r.client.Collection(CollectionSubscriptions).Where("ownerUid", "==", uid).Documents(ctx)
	defer iteratorDocs.Stop()
	var values []Subscription
	for {
		snapshot, err := iteratorDocs.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var value Subscription
		if err := snapshot.DataTo(&value); err != nil {
			return nil, err
		}
		if value.DisabledAt == nil {
			values = append(values, value)
		}
	}
	return values, nil
}

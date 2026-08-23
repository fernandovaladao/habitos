package reminder

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"habitos/internal/auth"
)

func TestDeliveryIdentityUsesOnlyApprovedFields(t *testing.T) {
	first := DeliveryID("u", "h", "2026-08-23", ChannelNotification)
	if first != DeliveryID("u", "h", "2026-08-23", ChannelNotification) {
		t.Fatal("identidade não é determinística")
	}
	if first == DeliveryID("u", "h", "2026-08-24", ChannelNotification) || first == DeliveryID("u", "h", "2026-08-23", ChannelEmail) {
		t.Fatal("data e canal devem distinguir entregas")
	}
	value := Delivery{ID: first, ScheduleVersionIDSnapshot: "v1", TimezoneSnapshot: "UTC"}
	value.ScheduleVersionIDSnapshot, value.TimezoneSnapshot = "v2", "America/Sao_Paulo"
	if value.ID != first {
		t.Fatal("metadados alteraram identidade")
	}
}

func TestResolveCivilTimeDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	nonexistent, ok := resolveCivilTime("2024-03-10", "02:30", location)
	if !ok || nonexistent.In(location).Format("15:04") != "03:00" {
		t.Fatalf("horário inexistente = %v", nonexistent.In(location))
	}
	ambiguous, ok := resolveCivilTime("2024-11-03", "01:30", location)
	if !ok || ambiguous.UTC().Format(time.RFC3339) != "2024-11-03T05:30:00Z" {
		t.Fatalf("primeira ocorrência ambígua = %s", ambiguous.UTC())
	}
}

type repositoryFake struct {
	delivery      Delivery
	eligibility   Eligibility
	physical      map[string]PhysicalDelivery
	finished      Status
	finishCode    string
	retried       int
	retryAt       time.Time
	subscriptions []Subscription
}

func (r *repositoryFake) ReconcileUser(context.Context, string, time.Time, bool) error { return nil }
func (r *repositoryFake) Due(context.Context, time.Time, int) ([]Delivery, error) {
	return []Delivery{r.delivery}, nil
}
func (r *repositoryFake) Claim(_ context.Context, _ string, _ string, now time.Time) (Delivery, bool, error) {
	if r.delivery.Status == StatusSent || r.delivery.Status == StatusSkipped || r.delivery.Status == StatusFailed || now.Before(r.delivery.NextAttemptAt) || !now.Before(r.delivery.ExpiresAt) {
		return r.delivery, false, nil
	}
	return r.delivery, true, nil
}
func (r *repositoryFake) Revalidate(context.Context, Delivery, time.Time) (Eligibility, error) {
	return r.eligibility, nil
}
func (r *repositoryFake) RecordPhysical(_ context.Context, _ string, id string, value PhysicalDelivery, _ time.Time) error {
	if r.physical == nil {
		r.physical = map[string]PhysicalDelivery{}
	}
	r.physical[id] = value
	if r.delivery.Physical == nil {
		r.delivery.Physical = map[string]PhysicalDelivery{}
	}
	r.delivery.Physical[id] = value
	return nil
}
func (r *repositoryFake) Finish(_ context.Context, _ string, status Status, code string, _ time.Time) error {
	r.finished, r.finishCode = status, code
	r.delivery.Status = status
	return nil
}
func (r *repositoryFake) Retry(_ context.Context, _ string, attempt int, at time.Time) error {
	r.retried, r.retryAt = attempt, at
	r.delivery.Status, r.delivery.AttemptCount, r.delivery.NextAttemptAt = StatusPending, attempt, at
	return nil
}
func (r *repositoryFake) UpsertSubscription(_ context.Context, value Subscription) (Subscription, error) {
	r.subscriptions = append(r.subscriptions, value)
	return value, nil
}
func (r *repositoryFake) DisableSubscription(context.Context, string, string, time.Time) error {
	return nil
}
func (r *repositoryFake) ListSubscriptions(context.Context, string) ([]Subscription, error) {
	return r.subscriptions, nil
}

type pushFake struct {
	calls     []string
	failures  map[string]error
	permanent map[string]bool
	sequences map[string][]pushResult
}
type pushResult struct {
	permanent bool
	err       error
}

func (p *pushFake) Send(_ context.Context, subscription Subscription, message PushMessage) (bool, error) {
	p.calls = append(p.calls, subscription.ID)
	if values := p.sequences[subscription.ID]; len(values) > 0 {
		result := values[0]
		p.sequences[subscription.ID] = values[1:]
		return result.permanent, result.err
	}
	return p.permanent[subscription.ID], p.failures[subscription.ID]
}

type emailFake struct {
	calls   int
	message EmailMessage
	err     error
}

func (e *emailFake) SendReminder(_ context.Context, message EmailMessage) error {
	e.calls++
	e.message = message
	return e.err
}

func TestNotificationTracksPhysicalDeliveryPerSubscription(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	repository := &repositoryFake{delivery: Delivery{ID: "logical", Channel: ChannelNotification, ScheduledAt: now, ExpiresAt: now.Add(30 * time.Minute), Physical: map[string]PhysicalDelivery{"already": {Status: PhysicalSent}}}, eligibility: Eligibility{Send: true, Subscriptions: []Subscription{{ID: "already"}, {ID: "new"}}}}
	push := &pushFake{}
	service := NewService(repository, &emailFake{}, push)
	service.now = func() time.Time { return now }
	if processed, err := service.Process(context.Background()); err != nil || processed != 1 {
		t.Fatalf("processado=%d erro=%v", processed, err)
	}
	if len(push.calls) != 1 || push.calls[0] != "new" {
		t.Fatalf("envios físicos=%v", push.calls)
	}
	if repository.physical["new"].Status != PhysicalSent || repository.finished != StatusSent {
		t.Fatalf("estado físico/lógico=%#v %s", repository.physical, repository.finished)
	}
}

func TestNotificationRetriesOnlyTransientDevices(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	transient := errors.New("temporário")
	repository := &repositoryFake{delivery: Delivery{ID: "logical", Channel: ChannelNotification, ScheduledAt: now, ExpiresAt: now.Add(30 * time.Minute), Physical: map[string]PhysicalDelivery{"ok": {Status: PhysicalSent}}}, eligibility: Eligibility{Send: true, Subscriptions: []Subscription{{ID: "ok"}, {ID: "retry"}}}}
	push := &pushFake{failures: map[string]error{"retry": transient}}
	service := NewService(repository, &emailFake{}, push)
	service.now = func() time.Time { return now }
	_, err := service.Process(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(push.calls) != 1 || push.calls[0] != "retry" || repository.retried != 1 || !repository.retryAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("calls=%v retry=%d em=%v", push.calls, repository.retried, repository.retryAt)
	}
}

func TestNotificationFanOutOutcomes(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	transient := errors.New("temporário")
	permanent := errors.New("expirada")
	tests := []struct {
		name         string
		push         *pushFake
		wantStatus   Status
		wantRetry    int
		wantPhysical map[string]PhysicalStatus
	}{
		{name: "duas com sucesso", push: &pushFake{}, wantStatus: StatusSent, wantPhysical: map[string]PhysicalStatus{"a": PhysicalSent, "b": PhysicalSent}},
		{name: "uma sucesso e uma transitória", push: &pushFake{failures: map[string]error{"b": transient}}, wantRetry: 1, wantPhysical: map[string]PhysicalStatus{"a": PhysicalSent, "b": PhysicalFailed}},
		{name: "uma expirada permanente", push: &pushFake{failures: map[string]error{"b": permanent}, permanent: map[string]bool{"b": true}}, wantStatus: StatusSent, wantPhysical: map[string]PhysicalStatus{"a": PhysicalSent, "b": PhysicalDisabled}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &repositoryFake{delivery: Delivery{ID: "logical", Channel: ChannelNotification, ScheduledAt: now, ExpiresAt: now.Add(30 * time.Minute), Physical: map[string]PhysicalDelivery{}}, eligibility: Eligibility{Send: true, Subscriptions: []Subscription{{ID: "a"}, {ID: "b"}}}}
			service := NewService(repository, &emailFake{}, test.push)
			service.now = func() time.Time { return now }
			if _, err := service.Process(context.Background()); err != nil {
				t.Fatal(err)
			}
			if repository.finished != test.wantStatus || repository.retried != test.wantRetry {
				t.Fatalf("status=%s retry=%d", repository.finished, repository.retried)
			}
			for id, want := range test.wantPhysical {
				if repository.physical[id].Status != want {
					t.Fatalf("%s=%s", id, repository.physical[id].Status)
				}
			}
		})
	}
}

func TestPushLostResponseCanRarelyDuplicateOnlyUnconfirmedDestination(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	lost := errors.New("resposta perdida")
	repository := &repositoryFake{delivery: Delivery{ID: "logical", Channel: ChannelNotification, ScheduledAt: now, ExpiresAt: now.Add(30 * time.Minute), Physical: map[string]PhysicalDelivery{}}, eligibility: Eligibility{Send: true, Subscriptions: []Subscription{{ID: "confirmed"}, {ID: "uncertain"}}}}
	push := &pushFake{sequences: map[string][]pushResult{"uncertain": {{err: lost}, {}}}}
	service := NewService(repository, &emailFake{}, push)
	service.now = func() time.Time { return now }
	if _, err := service.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(5 * time.Minute) }
	if _, err := service.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	confirmed, uncertain := 0, 0
	for _, id := range push.calls {
		if id == "confirmed" {
			confirmed++
		} else if id == "uncertain" {
			uncertain++
		}
	}
	if confirmed != 1 || uncertain != 2 {
		t.Fatalf("calls=%v", push.calls)
	}
}

func TestRetryScheduleAndExpiration(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	providerErr := errors.New("temporário")
	repository := &repositoryFake{delivery: Delivery{ID: "email-logical", Channel: ChannelEmail, ScheduledAt: base, ExpiresAt: base.Add(30 * time.Minute), NextAttemptAt: base, Message: MessageSnapshot{Recipient: "u@test", HabitTitle: "Ler", ScheduledTime: "12:00"}}, eligibility: Eligibility{Send: true}}
	email := &emailFake{err: providerErr}
	service := NewService(repository, email, &pushFake{})
	for _, instant := range []time.Time{base, base.Add(5 * time.Minute), base.Add(15 * time.Minute)} {
		service.now = func() time.Time { return instant }
		if _, err := service.Process(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if email.calls != 3 || repository.delivery.Status != StatusFailed {
		t.Fatalf("tentativas=%d status=%s", email.calls, repository.delivery.Status)
	}
	service.now = func() time.Time { return base.Add(30 * time.Minute) }
	if _, err := service.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	if email.calls != 3 {
		t.Fatalf("houve tentativa após T+30: %d", email.calls)
	}
}

func TestRetryRevalidatesExecutionBeforeCallingProviderAgain(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	repository := &repositoryFake{delivery: Delivery{ID: "logical", Channel: ChannelEmail, ScheduledAt: base, ExpiresAt: base.Add(30 * time.Minute), NextAttemptAt: base, Message: MessageSnapshot{Recipient: "u@test", HabitTitle: "Ler", ScheduledTime: "12:00"}}, eligibility: Eligibility{Send: true}}
	email := &emailFake{err: errors.New("temporário")}
	service := NewService(repository, email, &pushFake{})
	service.now = func() time.Time { return base }
	if _, err := service.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	repository.eligibility = Eligibility{SkipCode: "execution_resolved"}
	service.now = func() time.Time { return base.Add(5 * time.Minute) }
	if _, err := service.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	if email.calls != 1 || repository.delivery.Status != StatusSkipped || repository.finishCode != "execution_resolved" {
		t.Fatalf("calls=%d status=%s code=%s", email.calls, repository.delivery.Status, repository.finishCode)
	}
}

func TestEmailLostResponseReusesExactLogicalKeyAndSnapshot(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	lost := errors.New("resposta perdida")
	messages := []EmailMessage{}
	sender := emailSenderFunc(func(_ context.Context, value EmailMessage) error {
		messages = append(messages, value)
		if len(messages) == 1 {
			return lost
		}
		return nil
	})
	repository := &repositoryFake{delivery: Delivery{ID: "logical-key", Channel: ChannelEmail, ScheduledAt: base, ExpiresAt: base.Add(30 * time.Minute), NextAttemptAt: base, Message: MessageSnapshot{Recipient: "u@test", HabitTitle: "Ler", ScheduledTime: "12:00"}}, eligibility: Eligibility{Send: true}}
	service := NewService(repository, sender, &pushFake{})
	service.now = func() time.Time { return base }
	_, _ = service.Process(context.Background())
	service.now = func() time.Time { return base.Add(5 * time.Minute) }
	_, _ = service.Process(context.Background())
	if len(messages) != 2 || messages[0] != messages[1] || messages[0].IdempotencyKey != "logical-key" {
		t.Fatalf("mensagens=%#v", messages)
	}
}

type emailSenderFunc func(context.Context, EmailMessage) error

func (f emailSenderFunc) SendReminder(ctx context.Context, value EmailMessage) error {
	return f(ctx, value)
}

func TestSubscriptionRequiresAuthenticatedValidHTTPS(t *testing.T) {
	service := NewService(&repositoryFake{}, &emailFake{}, &pushFake{})
	if _, err := service.RegisterSubscription(context.Background(), auth.Identity{}, "https://push.test", "p", "a"); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("identidade vazia: %v", err)
	}
	if _, err := service.RegisterSubscription(context.Background(), auth.Identity{UID: "u"}, "http://push.test", "p", "a"); !errors.Is(err, ErrInvalidSubscription) {
		t.Fatalf("endpoint inseguro: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func TestResendUsesLogicalIdempotencyAndDeterministicSnapshot(t *testing.T) {
	var key, body string
	sender := NewResendSender("secret", "HÁBITOS <test@example.test>", "https://app.test", time.Second)
	sender.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		key = request.Header.Get("Idempotency-Key")
		data, _ := io.ReadAll(request.Body)
		body = string(data)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	err := sender.SendReminder(context.Background(), EmailMessage{To: "user@example.test", HabitTitle: "Ler", ScheduledTime: "19:00", IdempotencyKey: "logical-key"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "logical-key" || !strings.Contains(body, `Ler`) || !strings.Contains(body, `19:00`) {
		t.Fatalf("chave=%q corpo=%s", key, body)
	}
}

func TestResendClassifiesPermanentClientFailureWithoutExposingBody(t *testing.T) {
	sender := NewResendSender("secret", "test@example.test", "https://app.test", time.Second)
	sender.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnprocessableEntity, Body: io.NopCloser(strings.NewReader(`segredo externo`)), Header: make(http.Header)}, nil
	})}
	err := sender.SendReminder(context.Background(), EmailMessage{To: "user@example.test", HabitTitle: "Ler", ScheduledTime: "19:00", IdempotencyKey: "logical"})
	if !IsPermanent(err) || strings.Contains(err.Error(), "segredo externo") {
		t.Fatalf("erro permanente inseguro: %v", err)
	}
}

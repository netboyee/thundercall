package voicedispatcher

import (
	"context"
	"fmt"
	"testing"
	"time"

	twilioclient "github.com/twilio/twilio-go/client"

	"thundercall-go/internal/models"
	twilioprovider "thundercall-go/internal/providers/twilio"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
)

func TestServiceProcessAttemptMarksSentOnSuccess(t *testing.T) {
	t.Parallel()

	fixture := newServiceFixture()
	record := fixture.record(101, 201, 301, "+15550000010")

	if err := fixture.service.ProcessAttempt(context.Background(), record); err != nil {
		t.Fatalf("ProcessAttempt() error = %v", err)
	}

	if got := len(fixture.sender.calls); got != 1 {
		t.Fatalf("sender call count = %d, want 1", got)
	}
	if got := fixture.sender.calls[0].To; got != "+15550000010" {
		t.Fatalf("sender destination = %q, want intended destination", got)
	}
	if got := fixture.attempts.updatedStatuses[record.Attempt.ID]; got != "sent" {
		t.Fatalf("attempt status = %q, want sent", got)
	}
	if got := fixture.usersMessages.statuses[record.Attempt.UserMessageID]; got != "sent" {
		t.Fatalf("user message status = %q, want sent", got)
	}
}

func TestServiceProcessAttemptRequeuesRetryableTwilioErrors(t *testing.T) {
	t.Parallel()

	fixture := newServiceFixture()
	fixture.sender.sendErr = &twilioclient.TwilioRestError{Status: 429, Code: 20429, Message: "Too many requests"}
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return now }

	record := fixture.record(102, 202, 302, "+15550000020")
	if err := fixture.service.ProcessAttempt(context.Background(), record); err != nil {
		t.Fatalf("ProcessAttempt() error = %v", err)
	}

	if got := len(fixture.sender.calls); got != 1 {
		t.Fatalf("sender call count = %d, want 1", got)
	}
	if got := fixture.attempts.requeued[record.Attempt.ID]; got != now.Add(fixture.service.retryDelay) {
		t.Fatalf("dispatch_after = %s, want %s", got.Format(time.RFC3339), now.Add(fixture.service.retryDelay).Format(time.RFC3339))
	}
	if _, ok := fixture.attempts.updatedStatuses[record.Attempt.ID]; ok {
		t.Fatalf("attempt was marked final with status %q, want requeue only", fixture.attempts.updatedStatuses[record.Attempt.ID])
	}
	if got := fixture.notifications.statuses[*record.Attempt.NotificationID]; got != "queued" {
		t.Fatalf("notification status = %q, want queued", got)
	}
}

func TestServiceProcessAttemptCollapsesOverrideCallsAfterFirstSuccess(t *testing.T) {
	t.Parallel()

	fixture := newServiceFixture()
	fixture.sender.overrideTo = "+14073530340"
	fixture.sender.collapseOverride = true

	first := fixture.record(103, 203, 303, "+15550000030")
	second := fixture.record(104, 204, 304, "+15550000040")
	second.MessageID = first.MessageID

	if err := fixture.service.ProcessAttempt(context.Background(), first); err != nil {
		t.Fatalf("ProcessAttempt(first) error = %v", err)
	}
	if err := fixture.service.ProcessAttempt(context.Background(), second); err != nil {
		t.Fatalf("ProcessAttempt(second) error = %v", err)
	}

	if got := len(fixture.sender.calls); got != 1 {
		t.Fatalf("sender call count = %d, want 1", got)
	}
	if got := fixture.sender.calls[0].To; got != "+14073530340" {
		t.Fatalf("sender destination = %q, want override number", got)
	}
	if fixture.attempts.providerMessageIDs[first.Attempt.ID] != fixture.attempts.providerMessageIDs[second.Attempt.ID] {
		t.Fatalf(
			"provider message ids = %q and %q, want shared collapse id",
			fixture.attempts.providerMessageIDs[first.Attempt.ID],
			fixture.attempts.providerMessageIDs[second.Attempt.ID],
		)
	}
}

type serviceFixture struct {
	service       *Service
	attempts      *fakeAttemptsRepository
	usersMessages *fakeUsersMessagesRepository
	notifications *fakeNotificationsRepository
	sender        *fakeVoiceSender
}

func newServiceFixture() *serviceFixture {
	attempts := &fakeAttemptsRepository{
		updatedStatuses:    make(map[int64]string),
		providerMessageIDs: make(map[int64]string),
		requeued:           make(map[int64]time.Time),
		sentByMessage:      make(map[int64]string),
		attemptMessages:    make(map[int64]int64),
	}
	usersMessages := &fakeUsersMessagesRepository{statuses: make(map[int64]string)}
	notifications := &fakeNotificationsRepository{statuses: make(map[int64]string)}
	sender := &fakeVoiceSender{}

	service := NewService(attempts, usersMessages, notifications, sender, &fakeWaiter{}, 30*time.Second)
	service.logf = func(string, ...any) {}

	return &serviceFixture{
		service:       service,
		attempts:      attempts,
		usersMessages: usersMessages,
		notifications: notifications,
		sender:        sender,
	}
}

func (f *serviceFixture) record(attemptID int64, userMessageID int64, notificationID int64, destination string) deliveryattemptsrepo.VoiceDispatchRecord {
	record := deliveryattemptsrepo.VoiceDispatchRecord{
		Attempt: models.DeliveryAttempt{
			ID:             attemptID,
			UserMessageID:  userMessageID,
			NotificationID: int64Ptr(notificationID),
			Channel:        models.ChannelVoice,
			Destination:    destination,
			Status:         "dispatching",
			RequestedAt:    time.Date(2026, 8, 18, 11, 59, 0, 0, time.UTC),
		},
		MessageID:     9000 + attemptID,
		UserID:        8000 + attemptID,
		AccountID:     int64Ptr(42),
		EventCode:     "SVR",
		AlertTypeCode: "severe_thunderstorm_warning",
		MessageBody:   "Severe weather warning in your area.",
		MessageType:   "Severe Weather Warning",
		ReceivedAt:    time.Date(2026, 8, 18, 11, 58, 0, 0, time.UTC),
	}
	f.attempts.attemptMessages[attemptID] = record.MessageID
	return record
}

type fakeAttemptsRepository struct {
	updatedStatuses    map[int64]string
	providerMessageIDs map[int64]string
	requeued           map[int64]time.Time
	sentByMessage      map[int64]string
	attemptMessages    map[int64]int64
}

func (r *fakeAttemptsRepository) GetLatestSentVoiceAttemptByMessageID(_ context.Context, messageID int64) (*deliveryattemptsrepo.VoiceDispatchRecord, error) {
	providerID, ok := r.sentByMessage[messageID]
	if !ok {
		return nil, nil
	}
	return &deliveryattemptsrepo.VoiceDispatchRecord{
		Attempt: models.DeliveryAttempt{
			ProviderMessageID: stringPtr(providerID),
			Status:            "sent",
		},
		MessageID: messageID,
	}, nil
}

func (r *fakeAttemptsRepository) UpdateStatus(_ context.Context, id int64, status string, providerMessageID *string, _ *string, _ *time.Time, _ *time.Time) error {
	r.updatedStatuses[id] = status
	if providerMessageID != nil {
		r.providerMessageIDs[id] = *providerMessageID
		if status == "sent" {
			if messageID, ok := r.attemptMessages[id]; ok {
				r.sentByMessage[messageID] = *providerMessageID
			}
		}
	}
	return nil
}

func (r *fakeAttemptsRepository) Requeue(_ context.Context, id int64, _ *string, dispatchAfter time.Time) error {
	r.requeued[id] = dispatchAfter
	return nil
}

type fakeUsersMessagesRepository struct {
	statuses map[int64]string
}

func (r *fakeUsersMessagesRepository) UpdateStatus(_ context.Context, id int64, status string, _ *time.Time) error {
	r.statuses[id] = status
	return nil
}

type fakeNotificationsRepository struct {
	statuses map[int64]string
}

func (r *fakeNotificationsRepository) UpdateStatus(_ context.Context, id int64, status string, _ int64, _ *time.Time, _ *time.Time, _ *time.Time) error {
	r.statuses[id] = status
	return nil
}

type fakeVoiceSender struct {
	sendErr          error
	overrideTo       string
	collapseOverride bool
	calls            []twilioprovider.VoiceRequest
	nextProviderID   int
}

func (s *fakeVoiceSender) SendVoice(_ context.Context, request twilioprovider.VoiceRequest) (twilioprovider.Result, error) {
	s.calls = append(s.calls, request)
	if s.sendErr != nil {
		return twilioprovider.Result{}, s.sendErr
	}
	s.nextProviderID++
	providerID := fmt.Sprintf("call-%d", s.nextProviderID)
	return twilioprovider.Result{
		Provider:          "twilio_voice",
		ProviderMessageID: providerID,
		Status:            "queued",
	}, nil
}

func (s *fakeVoiceSender) ResolveVoiceDestination(to string) (string, bool) {
	if s.overrideTo == "" {
		return to, false
	}
	return s.overrideTo, true
}

func (s *fakeVoiceSender) BuildTestVoiceBody(intendedTo string, body string) string {
	return "TEST " + intendedTo + " " + body
}

func (s *fakeVoiceSender) BuildCollapsedTestVoiceBody(intendedTo string, body string) string {
	return "COLLAPSED " + intendedTo + " " + body
}

func (s *fakeVoiceSender) CollapseVoiceOverrideCalls() bool {
	return s.overrideTo != "" && s.collapseOverride
}

type fakeWaiter struct{}

func (w *fakeWaiter) Wait(_ context.Context) error {
	return nil
}

func int64Ptr(value int64) *int64 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

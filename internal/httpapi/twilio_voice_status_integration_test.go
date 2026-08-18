package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"thundercall-go/internal/models"
	twilioprovider "thundercall-go/internal/providers/twilio"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	messagesrepo "thundercall-go/internal/repositories/messages"
	notificationsrepo "thundercall-go/internal/repositories/notifications"
	nwseventsrepo "thundercall-go/internal/repositories/nwsevents"
	usersrepo "thundercall-go/internal/repositories/users"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
	"thundercall-go/internal/testmysql"
)

func TestHandleTwilioVoiceStatusCallbackMarksBusyAttemptFailed(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	fixture := seedTwilioVoiceCallbackFixture(t, ctx, harness)
	server := NewServer(harness.DB, time.Hour, nil)
	server.twilioAuthToken = "test-auth-token"

	form := url.Values{}
	form.Set("CallSid", "CA_busy")
	form.Set("CallStatus", "busy")
	form.Set("ErrorCode", "486")
	form.Set("SipResponseCode", "486")

	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/providers/twilio/voice/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", computeTwilioSignature(server.twilioAuthToken, twilioRequestURL(req), form))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204 response, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	attempt, err := deliveryattemptsrepo.New(harness.DB).GetByID(ctx, fixture.busyAttempt.ID)
	if err != nil {
		t.Fatalf("GetByID(busy attempt) error = %v", err)
	}
	if attempt == nil {
		t.Fatal("busy delivery attempt missing")
	}
	if attempt.Status != "failed" {
		t.Fatalf("attempt status = %q, want failed", attempt.Status)
	}
	if got := stringValue(attempt.ProviderStatus); got != "busy" {
		t.Fatalf("provider status = %q, want busy", got)
	}
	if got := stringValue(attempt.ErrorMessage); !strings.Contains(got, "busy") {
		t.Fatalf("error message = %q, want to contain busy", got)
	}
	if attempt.ProviderLastCallbackAt == nil {
		t.Fatal("expected provider_last_callback_at to be set")
	}
	var payload map[string][]string
	if err := json.Unmarshal([]byte(stringValue(attempt.ProviderPayloadJSON)), &payload); err != nil {
		t.Fatalf("unmarshal provider payload: %v", err)
	}
	if got := strings.Join(payload["CallStatus"], ","); got != "busy" {
		t.Fatalf("provider payload CallStatus = %q, want busy", got)
	}
	if got := strings.Join(payload["CallSid"], ","); got != "CA_busy" {
		t.Fatalf("provider payload CallSid = %q, want CA_busy", got)
	}

	userMessage, err := usersmessagesrepo.New(harness.DB).GetByMessageIDAndUserID(ctx, fixture.busyMessage.ID, fixture.user.ID)
	if err != nil {
		t.Fatalf("GetByMessageIDAndUserID(busy) error = %v", err)
	}
	if userMessage == nil {
		t.Fatal("busy user_message missing")
	}
	if userMessage.Status != "failed" {
		t.Fatalf("user_message status = %q, want failed", userMessage.Status)
	}
	if userMessage.DeliveredAt != nil {
		t.Fatalf("user_message delivered_at = %v, want nil after busy callback", *userMessage.DeliveredAt)
	}

	notification, err := notificationsrepo.New(harness.DB).GetByEventUserChannel(ctx, fixture.busyEvent.ID, fixture.user.ID, models.ChannelVoice)
	if err != nil {
		t.Fatalf("GetByEventUserChannel(busy) error = %v", err)
	}
	if notification == nil {
		t.Fatal("busy notification missing")
	}
	if notification.Status != "failed" {
		t.Fatalf("notification status = %q, want failed", notification.Status)
	}
}

func TestHandleTwilioVoiceStatusCallbackPersistsVoicemailOutcome(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	fixture := seedTwilioVoiceCallbackFixture(t, ctx, harness)
	server := NewServer(harness.DB, time.Hour, nil)
	server.twilioAuthToken = "test-auth-token"
	server.twilioVoice = stubTwilioVoiceLookup{
		details: twilioprovider.VoiceCallDetails{
			SID:             "CA_voicemail",
			Status:          "completed",
			AnsweredBy:      "machine_end_beep",
			DurationSeconds: intPtr(19),
		},
	}

	form := url.Values{}
	form.Set("CallSid", "CA_voicemail")
	form.Set("CallStatus", "completed")

	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/providers/twilio/voice/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", computeTwilioSignature(server.twilioAuthToken, twilioRequestURL(req), form))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204 response, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	attempt, err := deliveryattemptsrepo.New(harness.DB).GetByID(ctx, fixture.voicemailAttempt.ID)
	if err != nil {
		t.Fatalf("GetByID(voicemail attempt) error = %v", err)
	}
	if attempt == nil {
		t.Fatal("voicemail delivery attempt missing")
	}
	if attempt.Status != "sent" {
		t.Fatalf("attempt status = %q, want sent", attempt.Status)
	}
	if got := stringValue(attempt.ProviderStatus); got != "completed" {
		t.Fatalf("provider status = %q, want completed", got)
	}
	if got := stringValue(attempt.ProviderAnsweredBy); got != "machine_end_beep" {
		t.Fatalf("provider answered_by = %q, want machine_end_beep", got)
	}
	if got := intValue(attempt.ProviderDurationSeconds); got != 19 {
		t.Fatalf("provider duration seconds = %d, want 19", got)
	}
	if attempt.DeliveredAt == nil {
		t.Fatal("expected attempt delivered_at to be set for completed call")
	}

	userMessage, err := usersmessagesrepo.New(harness.DB).GetByMessageIDAndUserID(ctx, fixture.voicemailMessage.ID, fixture.user.ID)
	if err != nil {
		t.Fatalf("GetByMessageIDAndUserID(voicemail) error = %v", err)
	}
	if userMessage == nil {
		t.Fatal("voicemail user_message missing")
	}
	if userMessage.Status != "sent" {
		t.Fatalf("user_message status = %q, want sent", userMessage.Status)
	}
	if userMessage.DeliveredAt == nil {
		t.Fatal("expected user_message delivered_at to be set")
	}

	attemptItems, err := server.listDeliveryAttemptsByUserMessageIDs(ctx, []int64{userMessage.ID})
	if err != nil {
		t.Fatalf("listDeliveryAttemptsByUserMessageIDs() error = %v", err)
	}
	items := attemptItems[userMessage.ID]
	if len(items) != 1 {
		t.Fatalf("attempt item count = %d, want 1", len(items))
	}
	if got := stringValue(items[0].ProviderOutcome); got != "voicemail" {
		t.Fatalf("provider outcome = %q, want voicemail", got)
	}
}

type twilioVoiceCallbackFixture struct {
	busyEvent        *models.NWSEvent
	voicemailEvent   *models.NWSEvent
	user             *models.User
	busyMessage      *models.Message
	voicemailMessage *models.Message
	busyAttempt      *models.DeliveryAttempt
	voicemailAttempt *models.DeliveryAttempt
}

func seedTwilioVoiceCallbackFixture(t *testing.T, ctx context.Context, harness *testmysql.Harness) twilioVoiceCallbackFixture {
	t.Helper()

	account := &models.Account{Name: "Twilio Callback Test", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	user := &models.User{AccountID: account.ID, DisplayName: stringPtr("Callback User"), Active: true}
	if err := usersrepo.New(harness.DB).Create(ctx, user); err != nil {
		t.Fatalf("Create(user) error = %v", err)
	}

	busyEvent := createCallbackEvent(t, ctx, harness, "O:KTC1:SV:W:9999:2026", "9999")
	voicemailEvent := createCallbackEvent(t, ctx, harness, "O:KTC1:SV:W:1000:2026", "1000")

	busyMessage := createCallbackMessage(t, ctx, harness, account.ID, busyEvent.ID, "SVR", "busy-message")
	voicemailMessage := createCallbackMessage(t, ctx, harness, account.ID, voicemailEvent.ID, "SVR", "voicemail-message")

	busyUserMessage := createCallbackUserMessage(t, ctx, harness, busyMessage.ID, user.ID)
	voicemailUserMessage := createCallbackUserMessage(t, ctx, harness, voicemailMessage.ID, user.ID)

	busyNotification := createCallbackNotification(t, ctx, harness, busyEvent.ID, user.ID, busyMessage.ID)
	voicemailNotification := createCallbackNotification(t, ctx, harness, voicemailEvent.ID, user.ID, voicemailMessage.ID)

	busyAttempt := createCallbackAttempt(t, ctx, harness, busyUserMessage.ID, busyNotification.ID, "CA_busy")
	voicemailAttempt := createCallbackAttempt(t, ctx, harness, voicemailUserMessage.ID, voicemailNotification.ID, "CA_voicemail")

	return twilioVoiceCallbackFixture{
		busyEvent:        busyEvent,
		voicemailEvent:   voicemailEvent,
		user:             user,
		busyMessage:      busyMessage,
		voicemailMessage: voicemailMessage,
		busyAttempt:      busyAttempt,
		voicemailAttempt: voicemailAttempt,
	}
}

func createCallbackEvent(t *testing.T, ctx context.Context, harness *testmysql.Harness, eventKey string, etn string) *models.NWSEvent {
	t.Helper()

	event := &models.NWSEvent{
		EventKey:      eventKey,
		ProductClass:  "O",
		OfficeID:      "KTC1",
		Phenomenon:    "SV",
		Significance:  "W",
		ETN:           etn,
		EventYear:     2026,
		LastAction:    "NEW",
		FirstIssuedAt: timePtr(time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)),
		LastIssuedAt:  timePtr(time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)),
	}
	if err := nwseventsrepo.New(harness.DB).Create(ctx, event); err != nil {
		t.Fatalf("Create(event %q) error = %v", eventKey, err)
	}

	return event
}

func createCallbackMessage(t *testing.T, ctx context.Context, harness *testmysql.Harness, accountID int64, eventID int64, eventCode string, fingerprint string) *models.Message {
	t.Helper()

	message := &models.Message{
		AccountID:     &accountID,
		NWSEventID:    &eventID,
		Fingerprint:   fingerprint,
		Source:        "NWWS",
		EventCode:     eventCode,
		MessageType:   "Severe Weather Warning",
		AlertTypeCode: "severe_thunderstorm_warning",
		Body:          "callback test",
		Status:        "accepted",
		ReceivedAt:    time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC),
	}
	if err := messagesrepo.New(harness.DB).Create(ctx, message); err != nil {
		t.Fatalf("Create(message %q) error = %v", fingerprint, err)
	}
	return message
}

func createCallbackUserMessage(t *testing.T, ctx context.Context, harness *testmysql.Harness, messageID int64, userID int64) *models.UserMessage {
	t.Helper()

	userMessage := &models.UserMessage{
		MessageID:    messageID,
		UserID:       userID,
		VoiceEnabled: true,
		Status:       "sent",
		QueuedAt:     time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC),
		DeliveredAt:  timePtr(time.Date(2026, 8, 18, 15, 0, 5, 0, time.UTC)),
	}
	if err := usersmessagesrepo.New(harness.DB).Create(ctx, userMessage); err != nil {
		t.Fatalf("Create(user_message message=%d user=%d) error = %v", messageID, userID, err)
	}
	return userMessage
}

func createCallbackNotification(t *testing.T, ctx context.Context, harness *testmysql.Harness, eventID int64, userID int64, messageID int64) *models.Notification {
	t.Helper()

	notification := &models.Notification{
		NWSEventID:       eventID,
		UserID:           userID,
		Channel:          models.ChannelVoice,
		FirstMessageID:   messageID,
		LastMessageID:    messageID,
		Status:           "sent",
		FirstAttemptedAt: timePtr(time.Date(2026, 8, 18, 15, 0, 1, 0, time.UTC)),
		SentAt:           timePtr(time.Date(2026, 8, 18, 15, 0, 5, 0, time.UTC)),
	}
	if err := notificationsrepo.New(harness.DB).Create(ctx, notification); err != nil {
		t.Fatalf("Create(notification message=%d) error = %v", messageID, err)
	}
	return notification
}

func createCallbackAttempt(t *testing.T, ctx context.Context, harness *testmysql.Harness, userMessageID int64, notificationID int64, providerMessageID string) *models.DeliveryAttempt {
	t.Helper()

	attempt := &models.DeliveryAttempt{
		UserMessageID:     userMessageID,
		NotificationID:    &notificationID,
		Channel:           models.ChannelVoice,
		AttemptNumber:     1,
		Destination:       "+14075550000",
		Provider:          stringPtr("twilio_voice"),
		ProviderMessageID: &providerMessageID,
		Status:            "sent",
		RequestedAt:       time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC),
		DispatchAfter:     time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC),
		SentAt:            timePtr(time.Date(2026, 8, 18, 15, 0, 5, 0, time.UTC)),
	}
	if err := deliveryattemptsrepo.New(harness.DB).Create(ctx, attempt); err != nil {
		t.Fatalf("Create(delivery_attempt provider_message_id=%s) error = %v", providerMessageID, err)
	}
	return attempt
}

type stubTwilioVoiceLookup struct {
	details twilioprovider.VoiceCallDetails
	err     error
}

func (s stubTwilioVoiceLookup) LookupVoiceCall(_ context.Context, _ string) (twilioprovider.VoiceCallDetails, error) {
	return s.details, s.err
}

func stringPtr(value string) *string {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

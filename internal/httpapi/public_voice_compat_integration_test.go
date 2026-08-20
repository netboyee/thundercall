package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"thundercall-go/internal/models"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	locationsrepo "thundercall-go/internal/repositories/locations"
	messagesrepo "thundercall-go/internal/repositories/messages"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersrepo "thundercall-go/internal/repositories/users"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
	"thundercall-go/internal/testmysql"
)

func TestHandleGetLastPublicVoiceMessageReturnsLatestSentAttempt(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account, user := seedPublicVoiceAccountAndUser(t, ctx, harness, "+14073530340", "Latest Voice User")

	olderMessage := seedPublicVoiceMessage(t, ctx, harness, account.ID, "TOR", "tornado_warning", "older-message", time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC))
	olderUserMessage := seedPublicVoiceUserMessage(t, ctx, harness, olderMessage.ID, user.ID, "sent", time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC), timePtr(time.Date(2026, 8, 19, 10, 0, 5, 0, time.UTC)))
	seedPublicVoiceAttempt(t, ctx, harness, olderUserMessage.ID, "+14073530340", "sent", "CA_older", time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC), timePtr(time.Date(2026, 8, 19, 10, 0, 3, 0, time.UTC)), timePtr(time.Date(2026, 8, 19, 10, 0, 5, 0, time.UTC)))

	latestSentMessage := seedPublicVoiceMessage(t, ctx, harness, account.ID, "FLS", "flash_flood_warning", "latest-sent-message", time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	latestSentUserMessage := seedPublicVoiceUserMessage(t, ctx, harness, latestSentMessage.ID, user.ID, "sent", time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC), timePtr(time.Date(2026, 8, 20, 9, 0, 6, 0, time.UTC)))
	seedPublicVoiceAttempt(t, ctx, harness, latestSentUserMessage.ID, "+14073530340", "sent", "CA_latest_sent", time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC), timePtr(time.Date(2026, 8, 20, 9, 0, 4, 0, time.UTC)), timePtr(time.Date(2026, 8, 20, 9, 0, 6, 0, time.UTC)))

	newerFailedMessage := seedPublicVoiceMessage(t, ctx, harness, account.ID, "SVR", "severe_thunderstorm_warning", "newer-failed-message", time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC))
	newerFailedUserMessage := seedPublicVoiceUserMessage(t, ctx, harness, newerFailedMessage.ID, user.ID, "failed", time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC), nil)
	seedPublicVoiceAttempt(t, ctx, harness, newerFailedUserMessage.ID, "+14073530340", "failed", "CA_failed", time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC), nil, nil)

	server := NewServer(harness.DB, time.Hour, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/users/messages/last?phoneNumber=4073530340", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 response, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload publicLastVoiceMessageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !payload.Found {
		t.Fatal("expected found=true")
	}
	if payload.Loc == nil || *payload.Loc != account.ID {
		t.Fatalf("loc = %v, want %d", payload.Loc, account.ID)
	}
	if payload.AccountID == nil || *payload.AccountID != account.ID {
		t.Fatalf("accountId = %v, want %d", payload.AccountID, account.ID)
	}
	if payload.MessageID == nil || *payload.MessageID != latestSentMessage.ID {
		t.Fatalf("messageId = %v, want %d", payload.MessageID, latestSentMessage.ID)
	}
	if payload.Type != "FFW" {
		t.Fatalf("type = %q, want FFW", payload.Type)
	}
	if payload.EventCode != "FLS" {
		t.Fatalf("eventCode = %q, want FLS", payload.EventCode)
	}
	if payload.AlertTypeCode != "flash_flood_warning" {
		t.Fatalf("alertTypeCode = %q, want flash_flood_warning", payload.AlertTypeCode)
	}
	if payload.PhoneNumber != "+14073530340" {
		t.Fatalf("phoneNumber = %q, want +14073530340", payload.PhoneNumber)
	}
}

func TestHandleGetLastPublicVoiceMessageReturnsNotFoundPayload(t *testing.T) {
	server := NewServer(nil, time.Hour, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/users/messages/last?phoneNumber=4073530340", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 response, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload publicLastVoiceMessageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Found {
		t.Fatal("expected found=false")
	}
	if payload.Loc != nil {
		t.Fatalf("loc = %v, want nil", payload.Loc)
	}
	if payload.Type != "" {
		t.Fatalf("type = %q, want empty", payload.Type)
	}
	if payload.PhoneNumber != "+14073530340" {
		t.Fatalf("phoneNumber = %q, want +14073530340", payload.PhoneNumber)
	}
}

func TestHandlePublicVoiceOptOutDeactivatesMatchingUsers(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	_, firstUser := seedPublicVoiceAccountAndUser(t, ctx, harness, "+14073530340", "First Opt Out User")
	firstLocation := seedPublicVoiceLocation(t, ctx, harness, firstUser.AccountID, "First Location")
	seedPublicVoiceSubscription(t, ctx, harness, firstUser.ID, firstLocation.ID)
	seedPublicEmailMethod(t, ctx, harness, firstUser.ID, "first@example.com")

	_, secondUser := seedPublicVoiceAccountAndUser(t, ctx, harness, "+14073530340", "Second Opt Out User")
	secondLocation := seedPublicVoiceLocation(t, ctx, harness, secondUser.AccountID, "Second Location")
	seedPublicVoiceSubscription(t, ctx, harness, secondUser.ID, secondLocation.ID)

	_, untouchedUser := seedPublicVoiceAccountAndUser(t, ctx, harness, "+14075550111", "Untouched User")
	untouchedLocation := seedPublicVoiceLocation(t, ctx, harness, untouchedUser.AccountID, "Untouched Location")
	seedPublicVoiceSubscription(t, ctx, harness, untouchedUser.ID, untouchedLocation.ID)

	server := NewServer(harness.DB, time.Hour, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/users/voice/opt-out?phoneNumber=4073530340", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 response, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload publicVoiceOptOutResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !payload.Found {
		t.Fatal("expected found=true")
	}
	if payload.MatchedUsersCount != 2 {
		t.Fatalf("matchedUsersCount = %d, want 2", payload.MatchedUsersCount)
	}
	if payload.DeactivatedUsersCount != 2 {
		t.Fatalf("deactivatedUsersCount = %d, want 2", payload.DeactivatedUsersCount)
	}

	assertPublicVoiceUserInactive(t, ctx, harness, firstUser.ID)
	assertPublicVoiceUserInactive(t, ctx, harness, secondUser.ID)
	assertPublicVoiceUserStillActive(t, ctx, harness, untouchedUser.ID)

	activeMethods, err := usercontactmethodsrepo.New(harness.DB).ListByUserID(ctx, firstUser.ID)
	if err != nil {
		t.Fatalf("ListByUserID(firstUser) error = %v", err)
	}
	if len(activeMethods) != 1 {
		t.Fatalf("active contact method count = %d, want 1 after opt-out", len(activeMethods))
	}
	if activeMethods[0].Channel != models.ChannelEmail {
		t.Fatalf("remaining active contact method channel = %q, want %q", activeMethods[0].Channel, models.ChannelEmail)
	}
	if activeMethods[0].Destination != "first@example.com" {
		t.Fatalf("remaining active contact method destination = %q, want first@example.com", activeMethods[0].Destination)
	}

	subscriptions, err := userlocationsrepo.New(harness.DB).ListByUserID(ctx, firstUser.ID)
	if err != nil {
		t.Fatalf("ListByUserID(firstUser subscriptions) error = %v", err)
	}
	if len(subscriptions) != 1 || subscriptions[0].IsThunderCallEnabled {
		t.Fatalf("subscriptions after opt-out = %+v, want thundercall disabled", subscriptions)
	}

	untouchedMethods, err := usercontactmethodsrepo.New(harness.DB).ListByUserID(ctx, untouchedUser.ID)
	if err != nil {
		t.Fatalf("ListByUserID(untouchedUser) error = %v", err)
	}
	if len(untouchedMethods) == 0 {
		t.Fatal("expected untouched user to keep active contact methods")
	}
}

func TestHandlePublicVoiceOptOutRequiresPhoneNumber(t *testing.T) {
	server := NewServer(nil, time.Hour, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/users/voice/opt-out", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 response, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func seedPublicVoiceAccountAndUser(t *testing.T, ctx context.Context, harness *testmysql.Harness, phoneNumber string, displayName string) (*models.Account, *models.User) {
	t.Helper()

	account := &models.Account{Name: displayName + " Account", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	user := &models.User{
		AccountID:   account.ID,
		DisplayName: stringPtr(displayName),
		Active:      true,
	}
	if err := usersrepo.New(harness.DB).Create(ctx, user); err != nil {
		t.Fatalf("Create(user) error = %v", err)
	}

	method := &models.UserContactMethod{
		UserID:      user.ID,
		Channel:     models.ChannelVoice,
		Destination: phoneNumber,
		IsPrimary:   true,
		IsVerified:  false,
		Active:      true,
	}
	if err := usercontactmethodsrepo.New(harness.DB).Create(ctx, method); err != nil {
		t.Fatalf("Create(voice contact method) error = %v", err)
	}

	return account, user
}

func seedPublicEmailMethod(t *testing.T, ctx context.Context, harness *testmysql.Harness, userID int64, email string) {
	t.Helper()

	method := &models.UserContactMethod{
		UserID:      userID,
		Channel:     models.ChannelEmail,
		Destination: email,
		IsPrimary:   true,
		IsVerified:  false,
		Active:      true,
	}
	if err := usercontactmethodsrepo.New(harness.DB).Create(ctx, method); err != nil {
		t.Fatalf("Create(email contact method) error = %v", err)
	}
}

func seedPublicVoiceLocation(t *testing.T, ctx context.Context, harness *testmysql.Harness, accountID int64, name string) *models.Location {
	t.Helper()

	location := &models.Location{
		AccountID:            accountID,
		Name:                 name,
		IsThunderCallEnabled: true,
		Active:               true,
	}
	if err := locationsrepo.New(harness.DB).Create(ctx, location); err != nil {
		t.Fatalf("Create(location) error = %v", err)
	}
	return location
}

func seedPublicVoiceSubscription(t *testing.T, ctx context.Context, harness *testmysql.Harness, userID int64, locationID int64) {
	t.Helper()

	subscription := &models.UserLocation{
		UserID:               userID,
		LocationID:           locationID,
		SubscriptionType:     "address",
		IsPrimary:            true,
		IsThunderCallEnabled: true,
	}
	if err := userlocationsrepo.New(harness.DB).Create(ctx, subscription); err != nil {
		t.Fatalf("Create(subscription) error = %v", err)
	}
}

func seedPublicVoiceMessage(t *testing.T, ctx context.Context, harness *testmysql.Harness, accountID int64, eventCode string, alertTypeCode string, fingerprint string, receivedAt time.Time) *models.Message {
	t.Helper()

	message := &models.Message{
		AccountID:     &accountID,
		Fingerprint:   fingerprint,
		Source:        "NWWS",
		EventCode:     eventCode,
		MessageType:   "Public Voice Compatibility Test",
		AlertTypeCode: alertTypeCode,
		Body:          "public voice compatibility test body",
		Status:        "accepted",
		ReceivedAt:    receivedAt,
	}
	if err := messagesrepo.New(harness.DB).Create(ctx, message); err != nil {
		t.Fatalf("Create(message %q) error = %v", fingerprint, err)
	}
	return message
}

func seedPublicVoiceUserMessage(t *testing.T, ctx context.Context, harness *testmysql.Harness, messageID int64, userID int64, status string, queuedAt time.Time, deliveredAt *time.Time) *models.UserMessage {
	t.Helper()

	userMessage := &models.UserMessage{
		MessageID:    messageID,
		UserID:       userID,
		VoiceEnabled: true,
		Status:       status,
		QueuedAt:     queuedAt,
		DeliveredAt:  deliveredAt,
	}
	if err := usersmessagesrepo.New(harness.DB).Create(ctx, userMessage); err != nil {
		t.Fatalf("Create(user_message message=%d user=%d) error = %v", messageID, userID, err)
	}
	return userMessage
}

func seedPublicVoiceAttempt(t *testing.T, ctx context.Context, harness *testmysql.Harness, userMessageID int64, destination string, status string, providerMessageID string, requestedAt time.Time, sentAt *time.Time, deliveredAt *time.Time) *models.DeliveryAttempt {
	t.Helper()

	attempt := &models.DeliveryAttempt{
		UserMessageID:     userMessageID,
		Channel:           models.ChannelVoice,
		AttemptNumber:     1,
		Destination:       destination,
		Provider:          stringPtr("twilio_voice"),
		ProviderMessageID: stringPtr(providerMessageID),
		Status:            status,
		RequestedAt:       requestedAt,
		DispatchAfter:     requestedAt,
		SentAt:            sentAt,
		DeliveredAt:       deliveredAt,
	}
	if err := deliveryattemptsrepo.New(harness.DB).Create(ctx, attempt); err != nil {
		t.Fatalf("Create(delivery_attempt provider_message_id=%s) error = %v", providerMessageID, err)
	}
	return attempt
}

func assertPublicVoiceUserInactive(t *testing.T, ctx context.Context, harness *testmysql.Harness, userID int64) {
	t.Helper()

	user, err := usersrepo.New(harness.DB).GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID(user=%d) error = %v", userID, err)
	}
	if user == nil {
		t.Fatalf("user %d missing", userID)
	}
	if user.Active {
		t.Fatalf("user %d active = true, want false", userID)
	}
}

func assertPublicVoiceUserStillActive(t *testing.T, ctx context.Context, harness *testmysql.Harness, userID int64) {
	t.Helper()

	user, err := usersrepo.New(harness.DB).GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID(user=%d) error = %v", userID, err)
	}
	if user == nil {
		t.Fatalf("user %d missing", userID)
	}
	if !user.Active {
		t.Fatalf("user %d active = false, want true", userID)
	}
}

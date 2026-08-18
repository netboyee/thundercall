//go:build integration

package voicedispatcher

import (
	"context"
	"testing"
	"time"

	"thundercall-go/internal/models"
	twilioprovider "thundercall-go/internal/providers/twilio"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	accountsettingsrepo "thundercall-go/internal/repositories/accountsettings"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	locationsrepo "thundercall-go/internal/repositories/locations"
	messagesrepo "thundercall-go/internal/repositories/messages"
	notificationsrepo "thundercall-go/internal/repositories/notifications"
	nwseventsrepo "thundercall-go/internal/repositories/nwsevents"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersrepo "thundercall-go/internal/repositories/users"
	usersettingsrepo "thundercall-go/internal/repositories/usersettings"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
	"thundercall-go/internal/testmysql"
	"thundercall-go/internal/thundercall"
	"thundercall-go/internal/worker"
)

func TestMySQLIntegrationClaimQueuedVoiceAttemptsFairAcrossMessages(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	accountRepo := accountsrepo.New(harness.DB)
	userRepo := usersrepo.New(harness.DB)
	messageRepo := messagesrepo.New(harness.DB)
	userMessageRepo := usersmessagesrepo.New(harness.DB)
	attemptRepo := deliveryattemptsrepo.New(harness.DB)

	account := &models.Account{Name: "Voice Claim Fairness", Active: true}
	if err := accountRepo.Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	message1 := &models.Message{
		AccountID:     &account.ID,
		Fingerprint:   "m1",
		Source:        "NWWS",
		EventCode:     "SVR",
		MessageType:   "Severe Weather Warning",
		AlertTypeCode: "severe_thunderstorm_warning",
		Body:          "message one",
		PolygonWKT:    stringPtr("POLYGON ((39 -85,39 -84,40 -84,40 -85,39 -85))"),
		Status:        "accepted",
		ReceivedAt:    time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
	}
	message2 := &models.Message{
		AccountID:     &account.ID,
		Fingerprint:   "m2",
		Source:        "NWWS",
		EventCode:     "TOR",
		MessageType:   "Tornado Warning",
		AlertTypeCode: "tornado_warning",
		Body:          "message two",
		PolygonWKT:    stringPtr("POLYGON ((40 -85,40 -84,41 -84,41 -85,40 -85))"),
		Status:        "accepted",
		ReceivedAt:    time.Date(2026, 8, 18, 12, 1, 0, 0, time.UTC),
	}
	if err := messageRepo.Create(ctx, message1); err != nil {
		t.Fatalf("Create(message1) error = %v", err)
	}
	if err := messageRepo.Create(ctx, message2); err != nil {
		t.Fatalf("Create(message2) error = %v", err)
	}

	user1 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User 1")
	user2 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User 2")
	user3 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User 3")
	user4 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User 4")
	user5 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User 5")

	now := time.Date(2026, 8, 18, 12, 2, 0, 0, time.UTC)
	createQueuedVoiceAttempt(t, ctx, userMessageRepo, attemptRepo, message1.ID, user1.ID, "+15550000001", now)
	createQueuedVoiceAttempt(t, ctx, userMessageRepo, attemptRepo, message1.ID, user2.ID, "+15550000002", now.Add(1*time.Second))
	createQueuedVoiceAttempt(t, ctx, userMessageRepo, attemptRepo, message1.ID, user3.ID, "+15550000003", now.Add(2*time.Second))
	createQueuedVoiceAttempt(t, ctx, userMessageRepo, attemptRepo, message2.ID, user4.ID, "+15550000004", now.Add(3*time.Second))
	createQueuedVoiceAttempt(t, ctx, userMessageRepo, attemptRepo, message2.ID, user5.ID, "+15550000005", now.Add(4*time.Second))

	records, err := attemptRepo.ClaimQueuedVoiceAttempts(ctx, "lease-1", "dispatcher-1", now.Add(10*time.Second), time.Minute, 5)
	if err != nil {
		t.Fatalf("ClaimQueuedVoiceAttempts() error = %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("claimed records = %d, want 5", len(records))
	}

	got := []string{
		records[0].Attempt.Destination,
		records[1].Attempt.Destination,
		records[2].Attempt.Destination,
		records[3].Attempt.Destination,
		records[4].Attempt.Destination,
	}
	want := []string{
		"+15550000001",
		"+15550000004",
		"+15550000002",
		"+15550000005",
		"+15550000003",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claim order = %v, want %v", got, want)
		}
	}
}

func TestMySQLIntegrationWorkerAndVoiceDispatcherOnlyCallNetNewUsersOnUpdatedEvent(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	accountRepo := accountsrepo.New(harness.DB)
	userRepo := usersrepo.New(harness.DB)
	locationRepo := locationsrepo.New(harness.DB)
	userLocationRepo := userlocationsrepo.New(harness.DB)
	contactRepo := usercontactmethodsrepo.New(harness.DB)
	accountSettingsRepo := accountsettingsrepo.New(harness.DB)
	userSettingsRepo := usersettingsrepo.New(harness.DB)
	messageRepo := messagesrepo.New(harness.DB)
	eventRepo := nwseventsrepo.New(harness.DB)
	userMessageRepo := usersmessagesrepo.New(harness.DB)
	attemptRepo := deliveryattemptsrepo.New(harness.DB)
	notificationRepo := notificationsrepo.New(harness.DB)

	account := &models.Account{Name: "Voice End-to-End", Active: true}
	if err := accountRepo.Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	user1 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User One")
	user2 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User Two")
	user3 := createVoiceTestUser(t, ctx, userRepo, account.ID, "User Three")
	createVoiceContact(t, ctx, contactRepo, user1.ID, "+15550000001")
	createVoiceContact(t, ctx, contactRepo, user2.ID, "+15550000002")
	createVoiceContact(t, ctx, contactRepo, user3.ID, "+15550000003")

	location1 := createVoiceLocation(t, ctx, locationRepo, account.ID, "Loc 1", "POINT (39.20 -84.80)")
	location2 := createVoiceLocation(t, ctx, locationRepo, account.ID, "Loc 2", "POINT (39.70 -84.40)")
	location3 := createVoiceLocation(t, ctx, locationRepo, account.ID, "Loc 3", "POINT (40.20 -84.20)")
	createVoiceSubscription(t, ctx, userLocationRepo, user1.ID, location1.ID)
	createVoiceSubscription(t, ctx, userLocationRepo, user2.ID, location2.ID)
	createVoiceSubscription(t, ctx, userLocationRepo, user3.ID, location3.ID)

	event := &models.NWSEvent{
		EventKey:      "O:KTC1:SV:W:9001:2026",
		ProductClass:  "O",
		OfficeID:      "KTC1",
		Phenomenon:    "SV",
		Significance:  "W",
		ETN:           "9001",
		EventYear:     2026,
		LastAction:    "NEW",
		FirstIssuedAt: timePtr(time.Date(2026, 8, 18, 13, 0, 0, 0, time.UTC)),
		LastIssuedAt:  timePtr(time.Date(2026, 8, 18, 13, 0, 0, 0, time.UTC)),
	}
	if err := eventRepo.Create(ctx, event); err != nil {
		t.Fatalf("Create(event) error = %v", err)
	}

	initialMessage := &models.Message{
		AccountID:     &account.ID,
		NWSEventID:    &event.ID,
		Fingerprint:   thundercall.GenerateFingerprint("Severe Weather Warning", "POLYGON ((39.00 -85.00,39.00 -84.00,40.00 -84.00,40.00 -85.00,39.00 -85.00))", nil, nil),
		Source:        "NWWS",
		EventCode:     "SVR",
		MessageType:   "Severe Weather Warning",
		AlertTypeCode: "severe_thunderstorm_warning",
		Body:          "initial bulletin",
		PolygonWKT:    stringPtr("POLYGON ((39.00 -85.00,39.00 -84.00,40.00 -84.00,40.00 -85.00,39.00 -85.00))"),
		Status:        "accepted",
		ReceivedAt:    time.Date(2026, 8, 18, 13, 0, 0, 0, time.UTC),
	}
	updatedMessage := &models.Message{
		AccountID:     &account.ID,
		NWSEventID:    &event.ID,
		Fingerprint:   thundercall.GenerateFingerprint("Severe Weather Statement", "POLYGON ((39.50 -84.50,39.50 -83.50,40.50 -83.50,40.50 -84.50,39.50 -84.50))", nil, nil),
		Source:        "NWWS",
		EventCode:     "SVS",
		MessageType:   "Severe Weather Statement",
		AlertTypeCode: "severe_thunderstorm_warning",
		Body:          "updated bulletin",
		PolygonWKT:    stringPtr("POLYGON ((39.50 -84.50,39.50 -83.50,40.50 -83.50,40.50 -84.50,39.50 -84.50))"),
		Status:        "accepted",
		ReceivedAt:    time.Date(2026, 8, 18, 13, 5, 0, 0, time.UTC),
	}
	if err := messageRepo.Create(ctx, initialMessage); err != nil {
		t.Fatalf("Create(initialMessage) error = %v", err)
	}
	if err := messageRepo.Create(ctx, updatedMessage); err != nil {
		t.Fatalf("Create(updatedMessage) error = %v", err)
	}

	resolver := thundercall.NewSQLRecipientResolver(locationRepo, userLocationRepo, accountSettingsRepo, userSettingsRepo)
	planner := thundercall.NewChannelDispatcher(contactRepo, userMessageRepo, attemptRepo, notificationRepo)
	workerService := worker.NewService(messageRepo, resolver, planner)

	if err := workerService.ProcessMessage(ctx, initialMessage.ID); err != nil {
		t.Fatalf("ProcessMessage(initial) error = %v", err)
	}
	if err := workerService.ProcessMessage(ctx, updatedMessage.ID); err != nil {
		t.Fatalf("ProcessMessage(updated) error = %v", err)
	}

	sender := &fakeVoiceSender{}
	dispatcher := NewService(attemptRepo, userMessageRepo, notificationRepo, sender, &fakeWaiter{}, 30*time.Second)
	dispatcher.logf = func(string, ...any) {}

	records, err := attemptRepo.ClaimQueuedVoiceAttempts(ctx, "lease-e2e", "dispatcher-e2e", time.Now().UTC().Add(time.Hour), time.Minute, 10)
	if err != nil {
		t.Fatalf("ClaimQueuedVoiceAttempts() error = %v", err)
	}
	for _, record := range records {
		if err := dispatcher.ProcessAttempt(ctx, record); err != nil {
			t.Fatalf("ProcessAttempt(%d) error = %v", record.Attempt.ID, err)
		}
	}

	if got := len(sender.calls); got != 3 {
		t.Fatalf("sender call count = %d, want 3", got)
	}
	assertVoiceCallCount(t, sender.calls, "+15550000001", 1)
	assertVoiceCallCount(t, sender.calls, "+15550000002", 1)
	assertVoiceCallCount(t, sender.calls, "+15550000003", 1)

	assertVoiceUserMessageStatus(t, ctx, userMessageRepo, initialMessage.ID, user1.ID, "sent")
	assertVoiceUserMessageStatus(t, ctx, userMessageRepo, initialMessage.ID, user2.ID, "sent")
	assertVoiceUserMessageStatus(t, ctx, userMessageRepo, updatedMessage.ID, user2.ID, "suppressed")
	assertVoiceUserMessageStatus(t, ctx, userMessageRepo, updatedMessage.ID, user3.ID, "sent")

	assertVoiceNotificationWindow(t, ctx, notificationRepo, event.ID, user1.ID, initialMessage.ID, initialMessage.ID)
	assertVoiceNotificationWindow(t, ctx, notificationRepo, event.ID, user2.ID, initialMessage.ID, updatedMessage.ID)
	assertVoiceNotificationWindow(t, ctx, notificationRepo, event.ID, user3.ID, updatedMessage.ID, updatedMessage.ID)
}

func createVoiceTestUser(t *testing.T, ctx context.Context, repo *usersrepo.Repository, accountID int64, displayName string) *models.User {
	t.Helper()
	user := &models.User{AccountID: accountID, DisplayName: stringPtr(displayName), Active: true}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create(user %q) error = %v", displayName, err)
	}
	return user
}

func createVoiceContact(t *testing.T, ctx context.Context, repo *usercontactmethodsrepo.Repository, userID int64, destination string) {
	t.Helper()
	method := &models.UserContactMethod{UserID: userID, Channel: models.ChannelVoice, Destination: destination, IsPrimary: true, IsVerified: true, Active: true}
	if err := repo.Create(ctx, method); err != nil {
		t.Fatalf("Create(contact user=%d) error = %v", userID, err)
	}
}

func createVoiceLocation(t *testing.T, ctx context.Context, repo *locationsrepo.Repository, accountID int64, name string, coverageWKT string) *models.Location {
	t.Helper()
	location := &models.Location{AccountID: accountID, Name: name, CoverageWKT: stringPtr(coverageWKT), IsThunderCallEnabled: true, Active: true}
	if err := repo.Create(ctx, location); err != nil {
		t.Fatalf("Create(location %q) error = %v", name, err)
	}
	return location
}

func createVoiceSubscription(t *testing.T, ctx context.Context, repo *userlocationsrepo.Repository, userID int64, locationID int64) {
	t.Helper()
	subscription := &models.UserLocation{UserID: userID, LocationID: locationID, SubscriptionType: "direct", IsPrimary: true, IsThunderCallEnabled: true}
	if err := repo.Create(ctx, subscription); err != nil {
		t.Fatalf("Create(subscription user=%d location=%d) error = %v", userID, locationID, err)
	}
}

func createQueuedVoiceAttempt(t *testing.T, ctx context.Context, userMessageRepo *usersmessagesrepo.Repository, attemptRepo *deliveryattemptsrepo.Repository, messageID int64, userID int64, destination string, requestedAt time.Time) {
	t.Helper()
	userMessage := &models.UserMessage{MessageID: messageID, UserID: userID, VoiceEnabled: true, Status: "queued", QueuedAt: requestedAt}
	if err := userMessageRepo.Create(ctx, userMessage); err != nil {
		t.Fatalf("Create(user_message message=%d user=%d) error = %v", messageID, userID, err)
	}

	attempt := &models.DeliveryAttempt{
		UserMessageID: userMessage.ID,
		Channel:       models.ChannelVoice,
		AttemptNumber: 1,
		Destination:   destination,
		Provider:      stringPtr("twilio_voice"),
		Status:        "queued",
		RequestedAt:   requestedAt,
		DispatchAfter: requestedAt,
	}
	if err := attemptRepo.Create(ctx, attempt); err != nil {
		t.Fatalf("Create(delivery_attempt message=%d user=%d) error = %v", messageID, userID, err)
	}
}

func assertVoiceUserMessageStatus(t *testing.T, ctx context.Context, repo *usersmessagesrepo.Repository, messageID int64, userID int64, want string) {
	t.Helper()
	record, err := repo.GetByMessageIDAndUserID(ctx, messageID, userID)
	if err != nil {
		t.Fatalf("GetByMessageIDAndUserID(message=%d user=%d) error = %v", messageID, userID, err)
	}
	if record == nil {
		t.Fatalf("user_message missing for message=%d user=%d", messageID, userID)
	}
	if record.Status != want {
		t.Fatalf("user_message status for message=%d user=%d = %q, want %q", messageID, userID, record.Status, want)
	}
}

func assertVoiceCallCount(t *testing.T, calls []twilioprovider.VoiceRequest, destination string, want int) {
	t.Helper()
	got := 0
	for _, call := range calls {
		if call.To == destination {
			got++
		}
	}
	if got != want {
		t.Fatalf("call count for %s = %d, want %d", destination, got, want)
	}
}

func assertVoiceNotificationWindow(t *testing.T, ctx context.Context, repo *notificationsrepo.Repository, eventID int64, userID int64, wantFirstMessageID int64, wantLastMessageID int64) {
	t.Helper()

	notification, err := repo.GetByEventUserChannel(ctx, eventID, userID, models.ChannelVoice)
	if err != nil {
		t.Fatalf("GetByEventUserChannel(event=%d user=%d) error = %v", eventID, userID, err)
	}
	if notification == nil {
		t.Fatalf("notification missing for event=%d user=%d", eventID, userID)
	}
	if notification.FirstMessageID != wantFirstMessageID || notification.LastMessageID != wantLastMessageID {
		t.Fatalf(
			"notification message window for user=%d = %d..%d, want %d..%d",
			userID,
			notification.FirstMessageID,
			notification.LastMessageID,
			wantFirstMessageID,
			wantLastMessageID,
		)
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}

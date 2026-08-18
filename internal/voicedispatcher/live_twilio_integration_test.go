//go:build integration

package voicedispatcher

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"thundercall-go/internal/config"
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

const (
	runLiveTwilioTestEnv = "THUNDERCALL_RUN_LIVE_TWILIO_TEST"
	liveTwilioTestToEnv  = "THUNDERCALL_LIVE_TWILIO_TEST_TO"
)

func TestLiveTwilioCallsInitialEventAndSuppressesEXTForSameRecipient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Twilio integration test in short mode")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(runLiveTwilioTestEnv)), "1") &&
		!strings.EqualFold(strings.TrimSpace(os.Getenv(runLiveTwilioTestEnv)), "true") {
		t.Skipf("%s is not enabled; this test places one real Twilio voice call", runLiveTwilioTestEnv)
	}

	liveDestination := strings.TrimSpace(os.Getenv(liveTwilioTestToEnv))
	if liveDestination == "" {
		t.Skipf("%s is required; this test places one real Twilio voice call", liveTwilioTestToEnv)
	}

	accountSID := requiredEnvOrSkip(t, "TWILIO_ACCOUNT_SID")
	authToken := requiredEnvOrSkip(t, "TWILIO_AUTH_TOKEN")
	voiceFrom := requiredEnvOrSkip(t, "TWILIO_VOICE_FROM")

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
	deliveryAttemptRepo := deliveryattemptsrepo.New(harness.DB)
	notificationRepo := notificationsrepo.New(harness.DB)

	account := &models.Account{Name: "ThunderCall Live Twilio Test", Active: true}
	if err := accountRepo.Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	user := &models.User{
		AccountID:   account.ID,
		DisplayName: stringPtr("ThunderCall Live Test Recipient"),
		Active:      true,
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Create(user) error = %v", err)
	}

	contactMethod := &models.UserContactMethod{
		UserID:      user.ID,
		Channel:     models.ChannelVoice,
		Destination: liveDestination,
		IsPrimary:   true,
		IsVerified:  true,
		Active:      true,
	}
	if err := contactRepo.Create(ctx, contactMethod); err != nil {
		t.Fatalf("Create(contact method) error = %v", err)
	}

	location := &models.Location{
		AccountID:            account.ID,
		Name:                 "Live Twilio Test Location",
		CoverageWKT:          stringPtr("POINT (28.7100 -81.5300)"),
		IsThunderCallEnabled: true,
		Active:               true,
	}
	if err := locationRepo.Create(ctx, location); err != nil {
		t.Fatalf("Create(location) error = %v", err)
	}

	subscription := &models.UserLocation{
		UserID:               user.ID,
		LocationID:           location.ID,
		SubscriptionType:     "direct",
		IsPrimary:            true,
		IsThunderCallEnabled: true,
	}
	if err := userLocationRepo.Create(ctx, subscription); err != nil {
		t.Fatalf("Create(subscription) error = %v", err)
	}

	event := &models.NWSEvent{
		EventKey:      "O:KTC1:SV:W:9001:2026",
		ProductClass:  "O",
		OfficeID:      "KTC1",
		Phenomenon:    "SV",
		Significance:  "W",
		ETN:           "9001",
		EventYear:     2026,
		LastAction:    "NEW",
		FirstIssuedAt: timePtr(time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)),
		LastIssuedAt:  timePtr(time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)),
	}
	if err := eventRepo.Create(ctx, event); err != nil {
		t.Fatalf("Create(event) error = %v", err)
	}

	initialAction := "NEW"
	initialMessage := &models.Message{
		AccountID:      &account.ID,
		NWSEventID:     &event.ID,
		Fingerprint:    thundercall.GenerateFingerprint("Severe Weather Warning", "POLYGON ((28.6500 -81.6000,28.6500 -81.4500,28.7800 -81.4500,28.7800 -81.6000,28.6500 -81.6000))", nil, nil),
		Source:         "NWWS",
		EventCode:      "SVR",
		MessageType:    "Severe Weather Warning",
		AlertTypeCode:  "severe_thunderstorm_warning",
		Title:          stringPtr("ThunderCall Live Twilio Integration Test"),
		Body:           "ThunderCall live integration test. This is the first call for this event. If update suppression is working correctly, you should not receive a second call for the EXT update.",
		PolygonWKT:     stringPtr("POLYGON ((28.6500 -81.6000,28.6500 -81.4500,28.7800 -81.4500,28.7800 -81.6000,28.6500 -81.6000))"),
		PrimaryVTECRaw: stringPtr("/O.NEW.KTC1.SV.W.9001.260817T2000Z-260817T2030Z/"),
		VTECAction:     &initialAction,
		Status:         "accepted",
		ReceivedAt:     time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC),
	}
	if err := messageRepo.Create(ctx, initialMessage); err != nil {
		t.Fatalf("Create(initial message) error = %v", err)
	}

	updatedAction := "EXT"
	updatedMessage := &models.Message{
		AccountID:      &account.ID,
		NWSEventID:     &event.ID,
		Fingerprint:    thundercall.GenerateFingerprint("Severe Weather Statement", "POLYGON ((28.6800 -81.5800,28.6800 -81.4700,28.7600 -81.4700,28.7600 -81.5800,28.6800 -81.5800))", nil, nil),
		Source:         "NWWS",
		EventCode:      "SVS",
		MessageType:    "Severe Weather Statement",
		AlertTypeCode:  "severe_thunderstorm_warning",
		Title:          stringPtr("ThunderCall Live Twilio Integration Test EXT"),
		Body:           "ThunderCall live integration test EXT update. You should not hear this call.",
		PolygonWKT:     stringPtr("POLYGON ((28.6800 -81.5800,28.6800 -81.4700,28.7600 -81.4700,28.7600 -81.5800,28.6800 -81.5800))"),
		PrimaryVTECRaw: stringPtr("/O.EXT.KTC1.SV.W.9001.260817T2000Z-260817T2045Z/"),
		VTECAction:     &updatedAction,
		Status:         "accepted",
		ReceivedAt:     time.Date(2026, 8, 17, 20, 5, 0, 0, time.UTC),
	}
	if err := messageRepo.Create(ctx, updatedMessage); err != nil {
		t.Fatalf("Create(updated message) error = %v", err)
	}

	resolver := thundercall.NewSQLRecipientResolver(locationRepo, userLocationRepo, accountSettingsRepo, userSettingsRepo)
	planner := thundercall.NewChannelDispatcher(contactRepo, userMessageRepo, deliveryAttemptRepo, notificationRepo)
	workerService := worker.NewService(messageRepo, resolver, planner)
	if err := workerService.ProcessMessage(ctx, initialMessage.ID); err != nil {
		t.Fatalf("ProcessMessage(initial) error = %v", err)
	}
	if err := workerService.ProcessMessage(ctx, updatedMessage.ID); err != nil {
		t.Fatalf("ProcessMessage(updated) error = %v", err)
	}

	records, err := deliveryAttemptRepo.ClaimQueuedVoiceAttempts(ctx, "live-lease", "live-dispatcher", time.Now().UTC().Add(time.Hour), time.Minute, 10)
	if err != nil {
		t.Fatalf("ClaimQueuedVoiceAttempts() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("claimed records = %d, want 1 real call attempt", len(records))
	}

	dispatcher := NewService(
		deliveryAttemptRepo,
		userMessageRepo,
		notificationRepo,
		twilioprovider.New(config.TwilioConfig{
			AccountSID:   accountSID,
			AuthToken:    authToken,
			VoiceFrom:    voiceFrom,
			VoiceURL:     "",
			VoiceLogOnly: false,
		}),
		&fakeWaiter{},
		30*time.Second,
	)
	dispatcher.logf = t.Logf

	t.Logf("placing live Twilio call to %s from %s for initial message_id=%d", liveDestination, voiceFrom, initialMessage.ID)
	for _, record := range records {
		if err := dispatcher.ProcessAttempt(ctx, record); err != nil {
			t.Fatalf("ProcessAttempt(%d) error = %v", record.Attempt.ID, err)
		}
	}

	assertMessageProcessed(t, ctx, messageRepo, initialMessage.ID)
	assertMessageProcessed(t, ctx, messageRepo, updatedMessage.ID)

	initialUserMessage := requireUserMessage(t, ctx, userMessageRepo, initialMessage.ID, user.ID)
	if initialUserMessage.Status != "sent" {
		t.Fatalf("initial user_message status = %q, want sent", initialUserMessage.Status)
	}

	updatedUserMessage := requireUserMessage(t, ctx, userMessageRepo, updatedMessage.ID, user.ID)
	if updatedUserMessage.Status != "suppressed" {
		t.Fatalf("updated user_message status = %q, want suppressed", updatedUserMessage.Status)
	}

	notification, err := notificationRepo.GetByEventUserChannel(ctx, event.ID, user.ID, models.ChannelVoice)
	if err != nil {
		t.Fatalf("GetByEventUserChannel() error = %v", err)
	}
	if notification == nil {
		t.Fatal("notification missing after processing messages")
	}
	if notification.FirstMessageID != initialMessage.ID {
		t.Fatalf("notification first_message_id = %d, want %d", notification.FirstMessageID, initialMessage.ID)
	}
	if notification.LastMessageID != updatedMessage.ID {
		t.Fatalf("notification last_message_id = %d, want %d", notification.LastMessageID, updatedMessage.ID)
	}

	attempts, err := deliveryAttemptRepo.ListByNotificationID(ctx, notification.ID)
	if err != nil {
		t.Fatalf("ListByNotificationID() error = %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("delivery attempts count = %d, want 1 live call total", len(attempts))
	}

	attempt := attempts[0]
	if attempt.Status != "sent" {
		t.Fatalf("delivery attempt status = %q, want sent", attempt.Status)
	}
	if attempt.Destination != liveDestination {
		t.Fatalf("delivery attempt destination = %q, want %q", attempt.Destination, liveDestination)
	}
	if attempt.ProviderMessageID == nil || strings.TrimSpace(*attempt.ProviderMessageID) == "" {
		t.Fatal("delivery attempt provider_message_id is empty, want Twilio call SID")
	}

	t.Logf("live Twilio call placed successfully with provider_message_id=%s", *attempt.ProviderMessageID)
	assertCountLive(t, harness.DB, "SELECT COUNT(*) FROM notifications", 1)
	assertCountLive(t, harness.DB, "SELECT COUNT(*) FROM delivery_attempts", 1)
}

func requiredEnvOrSkip(t *testing.T, key string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("%s is required; this test places one real Twilio voice call", key)
	}
	return value
}

func requireUserMessage(t *testing.T, ctx context.Context, repo *usersmessagesrepo.Repository, messageID int64, userID int64) *models.UserMessage {
	t.Helper()

	record, err := repo.GetByMessageIDAndUserID(ctx, messageID, userID)
	if err != nil {
		t.Fatalf("GetByMessageIDAndUserID(message=%d user=%d) error = %v", messageID, userID, err)
	}
	if record == nil {
		t.Fatalf("user_message missing for message=%d user=%d", messageID, userID)
	}
	return record
}

func assertMessageProcessed(t *testing.T, ctx context.Context, repo *messagesrepo.Repository, messageID int64) {
	t.Helper()

	message, err := repo.GetByID(ctx, messageID)
	if err != nil {
		t.Fatalf("GetByID(message=%d) error = %v", messageID, err)
	}
	if message == nil {
		t.Fatalf("message %d missing", messageID)
	}
	if message.Status != "processed" {
		t.Fatalf("message %d status = %q, want processed", message.ID, message.Status)
	}
	if message.ProcessedAt == nil {
		t.Fatalf("message %d processed_at = nil, want timestamp", message.ID)
	}
}

func assertCountLive(t *testing.T, db *sql.DB, query string, want int64) {
	t.Helper()

	var got int64
	if err := db.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != want {
		t.Fatalf("count query %q = %d, want %d", query, got, want)
	}
}

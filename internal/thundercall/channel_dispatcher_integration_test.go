//go:build integration

package thundercall

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"thundercall-go/internal/models"
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
)

func TestMySQLIntegrationInitialAndUpdatedPolygonCallOnlyNetNewUsers(t *testing.T) {
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

	account := &models.Account{Name: "ThunderCall Integration", Active: true}
	if err := accountRepo.Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	user1 := createIntegrationUser(t, ctx, userRepo, contactRepo, account.ID, "User One", "+15550000001")
	user2 := createIntegrationUser(t, ctx, userRepo, contactRepo, account.ID, "User Two", "+15550000002")
	user3 := createIntegrationUser(t, ctx, userRepo, contactRepo, account.ID, "User Three", "+15550000003")

	location1 := createIntegrationLocation(t, ctx, locationRepo, account.ID, "Loc 1", "POINT (39.20 -84.80)")
	location2 := createIntegrationLocation(t, ctx, locationRepo, account.ID, "Loc 2", "POINT (39.70 -84.40)")
	location3 := createIntegrationLocation(t, ctx, locationRepo, account.ID, "Loc 3", "POINT (40.20 -84.20)")

	createIntegrationSubscription(t, ctx, userLocationRepo, user1.ID, location1.ID)
	createIntegrationSubscription(t, ctx, userLocationRepo, user2.ID, location2.ID)
	createIntegrationSubscription(t, ctx, userLocationRepo, user3.ID, location3.ID)

	event := &models.NWSEvent{
		EventKey:      "O:KTC1:SV:W:0001:2026",
		ProductClass:  "O",
		OfficeID:      "KTC1",
		Phenomenon:    "SV",
		Significance:  "W",
		ETN:           "0001",
		EventYear:     2026,
		LastAction:    "NEW",
		FirstIssuedAt: timePtr(time.Date(2026, 8, 17, 15, 0, 0, 0, time.UTC)),
		LastIssuedAt:  timePtr(time.Date(2026, 8, 17, 15, 0, 0, 0, time.UTC)),
	}
	if err := eventRepo.Create(ctx, event); err != nil {
		t.Fatalf("Create(event) error = %v", err)
	}

	initialMessage := &models.Message{
		AccountID:     &account.ID,
		NWSEventID:    &event.ID,
		Fingerprint:   GenerateFingerprint("Severe Weather Warning", "POLYGON ((39.00 -85.00,39.00 -84.00,40.00 -84.00,40.00 -85.00,39.00 -85.00))", nil, nil),
		Source:        "NWWS",
		EventCode:     "SVR",
		MessageType:   "Severe Weather Warning",
		AlertTypeCode: "severe_thunderstorm_warning",
		Body:          "initial bulletin",
		PolygonWKT:    stringPtrForIntegration("POLYGON ((39.00 -85.00,39.00 -84.00,40.00 -84.00,40.00 -85.00,39.00 -85.00))"),
		Status:        "accepted",
		ReceivedAt:    time.Date(2026, 8, 17, 15, 0, 0, 0, time.UTC),
	}
	if err := messageRepo.Create(ctx, initialMessage); err != nil {
		t.Fatalf("Create(initial message) error = %v", err)
	}

	updatedMessage := &models.Message{
		AccountID:     &account.ID,
		NWSEventID:    &event.ID,
		Fingerprint:   GenerateFingerprint("Severe Weather Warning", "POLYGON ((39.50 -84.50,39.50 -83.50,40.50 -83.50,40.50 -84.50,39.50 -84.50))", nil, nil),
		Source:        "NWWS",
		EventCode:     "SVS",
		MessageType:   "Severe Weather Statement",
		AlertTypeCode: "severe_thunderstorm_warning",
		Body:          "updated bulletin",
		PolygonWKT:    stringPtrForIntegration("POLYGON ((39.50 -84.50,39.50 -83.50,40.50 -83.50,40.50 -84.50,39.50 -84.50))"),
		Status:        "accepted",
		ReceivedAt:    time.Date(2026, 8, 17, 15, 5, 0, 0, time.UTC),
	}
	if err := messageRepo.Create(ctx, updatedMessage); err != nil {
		t.Fatalf("Create(updated message) error = %v", err)
	}

	resolver := NewSQLRecipientResolver(locationRepo, userLocationRepo, accountSettingsRepo, userSettingsRepo)

	dispatcher := NewChannelDispatcher(contactRepo, userMessageRepo, deliveryAttemptRepo, notificationRepo)
	dispatcher.now = func() time.Time { return time.Date(2026, 8, 17, 15, 10, 0, 0, time.UTC) }
	dispatcher.logf = func(string, ...any) {}

	initialMatches, err := resolver.ResolveRecipients(ctx, initialMessage)
	if err != nil {
		t.Fatalf("ResolveRecipients(initial) error = %v", err)
	}
	if got := sortedUserIDs(initialMatches); len(got) != 2 || got[0] != user1.ID || got[1] != user2.ID {
		t.Fatalf("initial user IDs = %v, want [%d %d]", got, user1.ID, user2.ID)
	}
	if err := dispatcher.Dispatch(ctx, initialMessage, initialMatches); err != nil {
		t.Fatalf("Dispatch(initial) error = %v", err)
	}

	updatedMatches, err := resolver.ResolveRecipients(ctx, updatedMessage)
	if err != nil {
		t.Fatalf("ResolveRecipients(updated) error = %v", err)
	}
	if got := sortedUserIDs(updatedMatches); len(got) != 2 || got[0] != user2.ID || got[1] != user3.ID {
		t.Fatalf("updated user IDs = %v, want [%d %d]", got, user2.ID, user3.ID)
	}
	if err := dispatcher.Dispatch(ctx, updatedMessage, updatedMatches); err != nil {
		t.Fatalf("Dispatch(updated) error = %v", err)
	}

	assertUserMessageStatus(t, ctx, userMessageRepo, updatedMessage.ID, user2.ID, "suppressed")
	assertUserMessageStatus(t, ctx, userMessageRepo, updatedMessage.ID, user3.ID, "queued")

	assertNotificationWindow(t, ctx, notificationRepo, deliveryAttemptRepo, event.ID, user1.ID, initialMessage.ID, initialMessage.ID)
	assertNotificationWindow(t, ctx, notificationRepo, deliveryAttemptRepo, event.ID, user2.ID, initialMessage.ID, updatedMessage.ID)
	assertNotificationWindow(t, ctx, notificationRepo, deliveryAttemptRepo, event.ID, user3.ID, updatedMessage.ID, updatedMessage.ID)

	assertCount(t, harness.DB, "SELECT COUNT(*) FROM notifications", 3)
	assertCount(t, harness.DB, "SELECT COUNT(*) FROM delivery_attempts", 3)
}

func createIntegrationUser(t *testing.T, ctx context.Context, userRepo *usersrepo.Repository, contactRepo *usercontactmethodsrepo.Repository, accountID int64, displayName string, destination string) *models.User {
	t.Helper()

	user := &models.User{
		AccountID:   accountID,
		DisplayName: stringPtrForIntegration(displayName),
		Active:      true,
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Create(user %q) error = %v", displayName, err)
	}

	method := &models.UserContactMethod{
		UserID:      user.ID,
		Channel:     models.ChannelVoice,
		Destination: destination,
		IsPrimary:   true,
		IsVerified:  true,
		Active:      true,
	}
	if err := contactRepo.Create(ctx, method); err != nil {
		t.Fatalf("Create(contact method for %q) error = %v", displayName, err)
	}

	return user
}

func createIntegrationLocation(t *testing.T, ctx context.Context, repo *locationsrepo.Repository, accountID int64, name string, coverageWKT string) *models.Location {
	t.Helper()

	location := &models.Location{
		AccountID:            accountID,
		Name:                 name,
		CoverageWKT:          stringPtrForIntegration(coverageWKT),
		IsThunderCallEnabled: true,
		Active:               true,
	}
	if err := repo.Create(ctx, location); err != nil {
		t.Fatalf("Create(location %q) error = %v", name, err)
	}
	return location
}

func createIntegrationSubscription(t *testing.T, ctx context.Context, repo *userlocationsrepo.Repository, userID int64, locationID int64) {
	t.Helper()

	subscription := &models.UserLocation{
		UserID:               userID,
		LocationID:           locationID,
		SubscriptionType:     "direct",
		IsPrimary:            true,
		IsThunderCallEnabled: true,
	}
	if err := repo.Create(ctx, subscription); err != nil {
		t.Fatalf("Create(subscription user=%d location=%d) error = %v", userID, locationID, err)
	}
}

func assertUserMessageStatus(t *testing.T, ctx context.Context, repo *usersmessagesrepo.Repository, messageID int64, userID int64, want string) {
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

func assertNotificationWindow(t *testing.T, ctx context.Context, repo *notificationsrepo.Repository, attempts *deliveryattemptsrepo.Repository, eventID int64, userID int64, wantFirstMessageID int64, wantLastMessageID int64) {
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
	if notification.Status != "queued" {
		t.Fatalf("notification status for user=%d = %q, want queued", userID, notification.Status)
	}

	deliveryAttempts, err := attempts.ListByNotificationID(ctx, notification.ID)
	if err != nil {
		t.Fatalf("ListByNotificationID(notification=%d) error = %v", notification.ID, err)
	}
	if len(deliveryAttempts) != 1 {
		t.Fatalf("delivery attempts for user=%d = %d, want 1", userID, len(deliveryAttempts))
	}
	if deliveryAttempts[0].Status != "queued" {
		t.Fatalf("delivery attempt status for user=%d = %q, want queued", userID, deliveryAttempts[0].Status)
	}
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("count query %q error = %v", query, err)
	}
	if got != want {
		t.Fatalf("count query %q = %d, want %d", query, got, want)
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func stringPtrForIntegration(value string) *string {
	return &value
}

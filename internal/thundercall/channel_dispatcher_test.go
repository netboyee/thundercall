package thundercall

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"

	"thundercall-go/internal/config"
	"thundercall-go/internal/models"
	twilioprovider "thundercall-go/internal/providers/twilio"
)

func TestChannelDispatcherDispatchDedupesLaterMessageForSameEvent(t *testing.T) {
	t.Parallel()

	fixture := newDispatcherFixture()
	eventID := int64(77)

	first := &models.Message{ID: 101, NWSEventID: &eventID, Body: "body", MessageType: "Severe Weather Warning"}
	second := &models.Message{ID: 102, NWSEventID: &eventID, Body: "body", MessageType: "Severe Weather Warning"}
	match := UserMatch{UserID: 10, LocationID: int64Ptr(1001), Channels: []models.Channel{models.ChannelVoice}}

	if err := fixture.dispatcher.Dispatch(context.Background(), first, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch(first) error = %v", err)
	}
	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count after first dispatch = %d, want 1", got)
	}

	if err := fixture.dispatcher.Dispatch(context.Background(), second, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch(second) error = %v", err)
	}
	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count after second dispatch = %d, want 1", got)
	}

	notification := fixture.notifications.onlyNotification(t)
	if notification.FirstMessageID != 101 || notification.LastMessageID != 102 {
		t.Fatalf("notification message span = %d..%d, want 101..102", notification.FirstMessageID, notification.LastMessageID)
	}
	if got := fixture.usersMessages.statusByMessageUser(102, 10); got != "suppressed" {
		t.Fatalf("second user message status = %q, want suppressed", got)
	}
	if got := len(fixture.deliveryAttempts.attempts); got != 1 {
		t.Fatalf("delivery attempt count = %d, want 1", got)
	}
}

func TestChannelDispatcherDispatchCallsOnlyNewUsersOnLaterMessage(t *testing.T) {
	t.Parallel()

	fixture := newDispatcherFixture()
	eventID := int64(88)

	first := &models.Message{ID: 201, NWSEventID: &eventID, Body: "body", MessageType: "Severe Weather Warning"}
	second := &models.Message{ID: 202, NWSEventID: &eventID, Body: "body", MessageType: "Severe Weather Warning"}

	if err := fixture.dispatcher.Dispatch(context.Background(), first, []UserMatch{
		{UserID: 10, LocationID: int64Ptr(2001), Channels: []models.Channel{models.ChannelVoice}},
	}); err != nil {
		t.Fatalf("Dispatch(first) error = %v", err)
	}
	if err := fixture.dispatcher.Dispatch(context.Background(), second, []UserMatch{
		{UserID: 10, LocationID: int64Ptr(2001), Channels: []models.Channel{models.ChannelVoice}},
		{UserID: 20, LocationID: int64Ptr(2002), Channels: []models.Channel{models.ChannelVoice}},
	}); err != nil {
		t.Fatalf("Dispatch(second) error = %v", err)
	}

	if got := len(fixture.sendCalls); got != 2 {
		t.Fatalf("send call count = %d, want 2", got)
	}
	if got := fixture.usersMessages.statusByMessageUser(202, 10); got != "suppressed" {
		t.Fatalf("existing user status = %q, want suppressed", got)
	}
	if got := fixture.usersMessages.statusByMessageUser(202, 20); got != "sent" {
		t.Fatalf("new user status = %q, want sent", got)
	}
}

func TestChannelDispatcherDispatchDoesNotRecallFailedNotificationOnLaterMessage(t *testing.T) {
	t.Parallel()

	fixture := newDispatcherFixture()
	fixture.sendErr = errors.New("twilio timeout")
	eventID := int64(99)

	first := &models.Message{ID: 301, NWSEventID: &eventID, Body: "body", MessageType: "Severe Weather Warning"}
	second := &models.Message{ID: 302, NWSEventID: &eventID, Body: "body", MessageType: "Severe Weather Warning"}
	match := UserMatch{UserID: 10, LocationID: int64Ptr(3001), Channels: []models.Channel{models.ChannelVoice}}

	if err := fixture.dispatcher.Dispatch(context.Background(), first, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch(first) error = %v", err)
	}
	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count after first dispatch = %d, want 1", got)
	}
	if got := fixture.notifications.onlyNotification(t).Status; got != "failed" {
		t.Fatalf("notification status after first dispatch = %q, want failed", got)
	}

	fixture.sendErr = nil
	if err := fixture.dispatcher.Dispatch(context.Background(), second, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch(second) error = %v", err)
	}
	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count after second dispatch = %d, want 1", got)
	}
	if got := fixture.usersMessages.statusByMessageUser(302, 10); got != "suppressed" {
		t.Fatalf("later user message status = %q, want suppressed", got)
	}
}

func TestChannelDispatcherDispatchFallsBackToPerMessageIdempotencyWithoutEvent(t *testing.T) {
	t.Parallel()

	fixture := newDispatcherFixture()
	message := &models.Message{ID: 401, Body: "body", MessageType: "Severe Weather Warning"}
	match := UserMatch{UserID: 10, LocationID: int64Ptr(4001), Channels: []models.Channel{models.ChannelVoice}}

	if err := fixture.dispatcher.Dispatch(context.Background(), message, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch(first) error = %v", err)
	}
	if err := fixture.dispatcher.Dispatch(context.Background(), message, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch(second) error = %v", err)
	}

	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count = %d, want 1", got)
	}
	if got := fixture.usersMessages.statusByMessageUser(401, 10); got != "sent" {
		t.Fatalf("user message status = %q, want sent", got)
	}
}

func TestChannelDispatcherDispatchCanSendWhenNotificationExistsWithoutAttempts(t *testing.T) {
	t.Parallel()

	fixture := newDispatcherFixture()
	eventID := int64(123)
	fixture.notifications.seed(&models.Notification{
		ID:             1,
		NWSEventID:     eventID,
		UserID:         10,
		Channel:        models.ChannelVoice,
		FirstMessageID: 501,
		LastMessageID:  501,
		Status:         "queued",
	})

	message := &models.Message{ID: 502, NWSEventID: &eventID, Body: "body", MessageType: "Severe Weather Warning"}
	match := UserMatch{UserID: 10, LocationID: int64Ptr(5001), Channels: []models.Channel{models.ChannelVoice}}

	if err := fixture.dispatcher.Dispatch(context.Background(), message, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count = %d, want 1", got)
	}
	if got := fixture.notifications.onlyNotification(t).LastMessageID; got != 502 {
		t.Fatalf("LastMessageID = %d, want 502", got)
	}
}

func TestChannelDispatcherDispatchOverridesVoiceDestinationForTestCalls(t *testing.T) {
	t.Parallel()

	fixture := newDispatcherFixture()
	fixture.dispatcher.twilio = twilioprovider.New(config.TwilioConfig{
		VoiceToOverride: "+14073530340",
	})

	message := &models.Message{ID: 601, Body: "Severe weather warning in your area.", MessageType: "Severe Weather Warning"}
	match := UserMatch{UserID: 10, LocationID: int64Ptr(6001), Channels: []models.Channel{models.ChannelVoice}}

	if err := fixture.dispatcher.Dispatch(context.Background(), message, []UserMatch{match}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count = %d, want 1", got)
	}
	if fixture.sendCalls[0] != "voice:+14073530340" {
		t.Fatalf("voice destination = %q, want override number", fixture.sendCalls[0])
	}
	if len(fixture.sendBodies) != 1 {
		t.Fatalf("send body count = %d, want 1", len(fixture.sendBodies))
	}
	if !strings.Contains(fixture.sendBodies[0], "This call is meant for 1 5 5 5 0 0 0 0 0 1 0.") {
		t.Fatalf("voice body = %q, want intended-recipient prefix", fixture.sendBodies[0])
	}

	attempt, err := fixture.deliveryAttempts.GetByUserMessageIDAndChannel(context.Background(), 1, models.ChannelVoice)
	if err != nil {
		t.Fatalf("GetByUserMessageIDAndChannel() error = %v", err)
	}
	if attempt == nil {
		t.Fatal("delivery attempt = nil, want attempt")
	}
	if attempt.Destination != "+14073530340" {
		t.Fatalf("attempt.Destination = %q, want override number", attempt.Destination)
	}
}

func TestChannelDispatcherDispatchCollapsesOverrideCallsPerMessage(t *testing.T) {
	t.Parallel()

	fixture := newDispatcherFixture()
	fixture.dispatcher.twilio = twilioprovider.New(config.TwilioConfig{
		VoiceToOverride:         "+14073530340",
		VoiceOverrideSingleCall: true,
	})

	message := &models.Message{ID: 602, Body: "Severe weather warning in your area.", MessageType: "Severe Weather Warning"}
	matches := []UserMatch{
		{UserID: 10, LocationID: int64Ptr(6001), Channels: []models.Channel{models.ChannelVoice}},
		{UserID: 20, LocationID: int64Ptr(6002), Channels: []models.Channel{models.ChannelVoice}},
	}

	if err := fixture.dispatcher.Dispatch(context.Background(), message, matches); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if got := len(fixture.sendCalls); got != 1 {
		t.Fatalf("send call count = %d, want 1", got)
	}
	if fixture.sendCalls[0] != "voice:+14073530340" {
		t.Fatalf("voice destination = %q, want override number", fixture.sendCalls[0])
	}
	if len(fixture.sendBodies) != 1 {
		t.Fatalf("send body count = %d, want 1", len(fixture.sendBodies))
	}
	if !strings.Contains(fixture.sendBodies[0], "This test call stands in for one or more intended recipients.") {
		t.Fatalf("voice body = %q, want collapsed-call prefix", fixture.sendBodies[0])
	}
	if !strings.Contains(fixture.sendBodies[0], "The first intended recipient is 1 5 5 5 0 0 0 0 0 1 0.") {
		t.Fatalf("voice body = %q, want first recipient prefix", fixture.sendBodies[0])
	}

	firstAttempt, err := fixture.deliveryAttempts.GetByUserMessageIDAndChannel(context.Background(), 1, models.ChannelVoice)
	if err != nil {
		t.Fatalf("GetByUserMessageIDAndChannel(first) error = %v", err)
	}
	secondAttempt, err := fixture.deliveryAttempts.GetByUserMessageIDAndChannel(context.Background(), 2, models.ChannelVoice)
	if err != nil {
		t.Fatalf("GetByUserMessageIDAndChannel(second) error = %v", err)
	}
	if firstAttempt == nil || secondAttempt == nil {
		t.Fatal("delivery attempts = nil, want attempts for both recipients")
	}
	if firstAttempt.Status != "sent" || secondAttempt.Status != "sent" {
		t.Fatalf("attempt statuses = %q and %q, want both sent", firstAttempt.Status, secondAttempt.Status)
	}
	if firstAttempt.Destination != "+14073530340" || secondAttempt.Destination != "+14073530340" {
		t.Fatalf("attempt destinations = %q and %q, want override number", firstAttempt.Destination, secondAttempt.Destination)
	}
	if firstAttempt.ProviderMessageID == nil || secondAttempt.ProviderMessageID == nil {
		t.Fatal("provider message ids = nil, want shared provider ids")
	}
	if *firstAttempt.ProviderMessageID != *secondAttempt.ProviderMessageID {
		t.Fatalf("provider message ids = %q and %q, want shared id", *firstAttempt.ProviderMessageID, *secondAttempt.ProviderMessageID)
	}
	if got := fixture.usersMessages.statusByMessageUser(602, 10); got != "sent" {
		t.Fatalf("first user status = %q, want sent", got)
	}
	if got := fixture.usersMessages.statusByMessageUser(602, 20); got != "sent" {
		t.Fatalf("second user status = %q, want sent", got)
	}
}

type dispatcherFixture struct {
	dispatcher       *ChannelDispatcher
	contacts         *fakeContactMethodsRepository
	usersMessages    *fakeUsersMessagesRepository
	deliveryAttempts *fakeDeliveryAttemptsRepository
	notifications    *fakeNotificationsRepository
	sendCalls        []string
	sendBodies       []string
	sendErr          error
}

func newDispatcherFixture() *dispatcherFixture {
	contacts := &fakeContactMethodsRepository{
		byUser: map[int64][]models.UserContactMethod{
			10: {
				{UserID: 10, Channel: models.ChannelVoice, Destination: "+15550000010", Active: true},
			},
			20: {
				{UserID: 20, Channel: models.ChannelVoice, Destination: "+15550000020", Active: true},
			},
		},
	}
	usersMessages := newFakeUsersMessagesRepository()
	deliveryAttempts := newFakeDeliveryAttemptsRepository()
	notifications := newFakeNotificationsRepository()

	fixture := &dispatcherFixture{
		contacts:         contacts,
		usersMessages:    usersMessages,
		deliveryAttempts: deliveryAttempts,
		notifications:    notifications,
	}
	fixture.dispatcher = &ChannelDispatcher{
		contactMethods:   contacts,
		usersMessages:    usersMessages,
		deliveryAttempts: deliveryAttempts,
		notifications:    notifications,
		now:              func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) },
		logf:             func(string, ...any) {},
	}
	fixture.dispatcher.deliver = func(_ context.Context, channel models.Channel, destination string, message *models.Message, _ UserMatch) (deliveryResult, error) {
		fixture.sendCalls = append(fixture.sendCalls, fmt.Sprintf("%s:%s", channel, destination))
		fixture.sendBodies = append(fixture.sendBodies, message.Body)
		if fixture.sendErr != nil {
			return deliveryResult{}, fixture.sendErr
		}
		return deliveryResult{Provider: providerName(channel), ProviderMessageID: fmt.Sprintf("provider-%d", len(fixture.sendCalls)), Status: "sent"}, nil
	}
	return fixture
}

type fakeContactMethodsRepository struct {
	byUser map[int64][]models.UserContactMethod
}

func (r *fakeContactMethodsRepository) ListByUserIDs(_ context.Context, userIDs []int64) ([]models.UserContactMethod, error) {
	var result []models.UserContactMethod
	for _, userID := range userIDs {
		result = append(result, r.byUser[userID]...)
	}
	return result, nil
}

type userMessageKey struct {
	messageID int64
	userID    int64
}

type fakeUsersMessagesRepository struct {
	nextID    int64
	byKey     map[userMessageKey]*models.UserMessage
	byID      map[int64]*models.UserMessage
	statusLog map[int64]string
}

func newFakeUsersMessagesRepository() *fakeUsersMessagesRepository {
	return &fakeUsersMessagesRepository{
		byKey:     make(map[userMessageKey]*models.UserMessage),
		byID:      make(map[int64]*models.UserMessage),
		statusLog: make(map[int64]string),
	}
}

func (r *fakeUsersMessagesRepository) CreateOrGet(_ context.Context, userMessage *models.UserMessage) error {
	key := userMessageKey{messageID: userMessage.MessageID, userID: userMessage.UserID}
	if existing, ok := r.byKey[key]; ok {
		*userMessage = *existing
		return nil
	}

	r.nextID++
	copy := *userMessage
	copy.ID = r.nextID
	r.byKey[key] = &copy
	r.byID[copy.ID] = &copy
	*userMessage = copy
	return nil
}

func (r *fakeUsersMessagesRepository) UpdateStatus(_ context.Context, id int64, status string, deliveredAt *time.Time) error {
	existing := r.byID[id]
	if existing == nil {
		return fmt.Errorf("unknown user message %d", id)
	}
	existing.Status = status
	existing.DeliveredAt = deliveredAt
	r.statusLog[id] = status
	return nil
}

func (r *fakeUsersMessagesRepository) statusByMessageUser(messageID int64, userID int64) string {
	existing := r.byKey[userMessageKey{messageID: messageID, userID: userID}]
	if existing == nil {
		return ""
	}
	return existing.Status
}

type fakeDeliveryAttemptsRepository struct {
	nextID   int64
	attempts map[int64]*models.DeliveryAttempt
}

func newFakeDeliveryAttemptsRepository() *fakeDeliveryAttemptsRepository {
	return &fakeDeliveryAttemptsRepository{
		attempts: make(map[int64]*models.DeliveryAttempt),
	}
}

func (r *fakeDeliveryAttemptsRepository) Create(_ context.Context, attempt *models.DeliveryAttempt) error {
	for _, existing := range r.attempts {
		if existing.UserMessageID == attempt.UserMessageID && existing.Channel == attempt.Channel && existing.AttemptNumber == attempt.AttemptNumber {
			return &mysql.MySQLError{Number: 1062}
		}
		if existing.NotificationID != nil && attempt.NotificationID != nil && *existing.NotificationID == *attempt.NotificationID && existing.AttemptNumber == attempt.AttemptNumber {
			return &mysql.MySQLError{Number: 1062}
		}
	}

	r.nextID++
	copy := *attempt
	copy.ID = r.nextID
	r.attempts[copy.ID] = &copy
	*attempt = copy
	return nil
}

func (r *fakeDeliveryAttemptsRepository) GetByUserMessageIDAndChannel(_ context.Context, userMessageID int64, channel models.Channel) (*models.DeliveryAttempt, error) {
	var latest *models.DeliveryAttempt
	for _, attempt := range r.attempts {
		if attempt.UserMessageID != userMessageID || attempt.Channel != channel {
			continue
		}
		if latest == nil || attempt.AttemptNumber > latest.AttemptNumber || (attempt.AttemptNumber == latest.AttemptNumber && attempt.ID > latest.ID) {
			copy := *attempt
			latest = &copy
		}
	}
	return latest, nil
}

func (r *fakeDeliveryAttemptsRepository) ListByUserMessageID(_ context.Context, userMessageID int64) ([]models.DeliveryAttempt, error) {
	var result []models.DeliveryAttempt
	for _, attempt := range r.attempts {
		if attempt.UserMessageID == userMessageID {
			result = append(result, *attempt)
		}
	}
	return result, nil
}

func (r *fakeDeliveryAttemptsRepository) ListByNotificationID(_ context.Context, notificationID int64) ([]models.DeliveryAttempt, error) {
	var result []models.DeliveryAttempt
	for _, attempt := range r.attempts {
		if attempt.NotificationID != nil && *attempt.NotificationID == notificationID {
			result = append(result, *attempt)
		}
	}
	return result, nil
}

func (r *fakeDeliveryAttemptsRepository) UpdateStatus(_ context.Context, id int64, status string, providerMessageID *string, errorMessage *string, sentAt *time.Time, deliveredAt *time.Time) error {
	attempt := r.attempts[id]
	if attempt == nil {
		return fmt.Errorf("unknown attempt %d", id)
	}
	attempt.Status = status
	attempt.ProviderMessageID = providerMessageID
	attempt.ErrorMessage = errorMessage
	attempt.SentAt = sentAt
	attempt.DeliveredAt = deliveredAt
	return nil
}

func (r *fakeDeliveryAttemptsRepository) UpdateDestination(_ context.Context, id int64, destination string) error {
	attempt := r.attempts[id]
	if attempt == nil {
		return fmt.Errorf("unknown attempt %d", id)
	}
	attempt.Destination = destination
	return nil
}

type notificationKey struct {
	eventID int64
	userID  int64
	channel models.Channel
}

type fakeNotificationsRepository struct {
	nextID int64
	byKey  map[notificationKey]*models.Notification
	byID   map[int64]*models.Notification
}

func newFakeNotificationsRepository() *fakeNotificationsRepository {
	return &fakeNotificationsRepository{
		byKey: make(map[notificationKey]*models.Notification),
		byID:  make(map[int64]*models.Notification),
	}
}

func (r *fakeNotificationsRepository) seed(notification *models.Notification) {
	if notification.ID > r.nextID {
		r.nextID = notification.ID
	}
	copy := *notification
	r.byKey[notificationKey{eventID: notification.NWSEventID, userID: notification.UserID, channel: notification.Channel}] = &copy
	r.byID[notification.ID] = &copy
}

func (r *fakeNotificationsRepository) CreateOrGet(_ context.Context, notification *models.Notification) (bool, error) {
	key := notificationKey{eventID: notification.NWSEventID, userID: notification.UserID, channel: notification.Channel}
	if existing, ok := r.byKey[key]; ok {
		*notification = *existing
		return false, nil
	}

	r.nextID++
	copy := *notification
	copy.ID = r.nextID
	r.byKey[key] = &copy
	r.byID[copy.ID] = &copy
	*notification = copy
	return true, nil
}

func (r *fakeNotificationsRepository) TouchLastMessage(_ context.Context, id int64, lastMessageID int64) error {
	existing := r.byID[id]
	if existing == nil {
		return fmt.Errorf("unknown notification %d", id)
	}
	existing.LastMessageID = lastMessageID
	return nil
}

func (r *fakeNotificationsRepository) UpdateStatus(_ context.Context, id int64, status string, lastMessageID int64, firstAttemptedAt *time.Time, sentAt *time.Time, deliveredAt *time.Time) error {
	existing := r.byID[id]
	if existing == nil {
		return fmt.Errorf("unknown notification %d", id)
	}
	existing.Status = status
	existing.LastMessageID = lastMessageID
	if existing.FirstAttemptedAt == nil && firstAttemptedAt != nil {
		existing.FirstAttemptedAt = firstAttemptedAt
	}
	if existing.SentAt == nil && sentAt != nil {
		existing.SentAt = sentAt
	}
	if existing.DeliveredAt == nil && deliveredAt != nil {
		existing.DeliveredAt = deliveredAt
	}
	return nil
}

func (r *fakeNotificationsRepository) onlyNotification(t *testing.T) *models.Notification {
	t.Helper()
	if len(r.byID) != 1 {
		t.Fatalf("notification count = %d, want 1", len(r.byID))
	}
	for _, notification := range r.byID {
		copy := *notification
		return &copy
	}
	return nil
}

func int64Ptr(value int64) *int64 {
	return &value
}

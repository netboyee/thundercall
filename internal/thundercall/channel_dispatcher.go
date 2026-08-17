package thundercall

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"thundercall-go/internal/models"
	sendgridprovider "thundercall-go/internal/providers/sendgrid"
	twilioprovider "thundercall-go/internal/providers/twilio"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	notificationsrepo "thundercall-go/internal/repositories/notifications"
	"thundercall-go/internal/repositories/sqlutil"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
)

type contactMethodsRepository interface {
	ListByUserIDs(ctx context.Context, userIDs []int64) ([]models.UserContactMethod, error)
}

type usersMessagesRepository interface {
	CreateOrGet(ctx context.Context, userMessage *models.UserMessage) error
	UpdateStatus(ctx context.Context, id int64, status string, deliveredAt *time.Time) error
}

type deliveryAttemptsRepository interface {
	Create(ctx context.Context, attempt *models.DeliveryAttempt) error
	GetByUserMessageIDAndChannel(ctx context.Context, userMessageID int64, channel models.Channel) (*models.DeliveryAttempt, error)
	ListByUserMessageID(ctx context.Context, userMessageID int64) ([]models.DeliveryAttempt, error)
	ListByNotificationID(ctx context.Context, notificationID int64) ([]models.DeliveryAttempt, error)
	UpdateStatus(ctx context.Context, id int64, status string, providerMessageID *string, errorMessage *string, sentAt *time.Time, deliveredAt *time.Time) error
}

type notificationsRepository interface {
	CreateOrGet(ctx context.Context, notification *models.Notification) (bool, error)
	TouchLastMessage(ctx context.Context, id int64, lastMessageID int64) error
	UpdateStatus(ctx context.Context, id int64, status string, lastMessageID int64, firstAttemptedAt *time.Time, sentAt *time.Time, deliveredAt *time.Time) error
}

type sendChannelFunc func(ctx context.Context, channel models.Channel, destination string, message *models.Message) (deliveryResult, error)

type ChannelDispatcher struct {
	contactMethods   contactMethodsRepository
	usersMessages    usersMessagesRepository
	deliveryAttempts deliveryAttemptsRepository
	notifications    notificationsRepository
	twilio           *twilioprovider.Provider
	sendgrid         *sendgridprovider.Provider
	deliver          sendChannelFunc
	now              func() time.Time
	logf             func(string, ...any)
}

func NewChannelDispatcher(
	contactMethods *usercontactmethodsrepo.Repository,
	usersMessages *usersmessagesrepo.Repository,
	deliveryAttempts *deliveryattemptsrepo.Repository,
	notifications *notificationsrepo.Repository,
	twilio *twilioprovider.Provider,
	sendgrid *sendgridprovider.Provider,
) *ChannelDispatcher {
	dispatcher := &ChannelDispatcher{
		contactMethods:   contactMethods,
		usersMessages:    usersMessages,
		deliveryAttempts: deliveryAttempts,
		notifications:    notifications,
		twilio:           twilio,
		sendgrid:         sendgrid,
		now:              func() time.Time { return time.Now().UTC() },
		logf:             log.Printf,
	}
	dispatcher.deliver = dispatcher.sendWithProviders
	return dispatcher
}

func (d *ChannelDispatcher) Dispatch(ctx context.Context, message *models.Message, matches []UserMatch) error {
	if d.contactMethods == nil || d.usersMessages == nil || d.deliveryAttempts == nil || len(matches) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(matches))
	for _, match := range matches {
		userIDs = append(userIDs, match.UserID)
	}

	methods, err := d.contactMethods.ListByUserIDs(ctx, userIDs)
	if err != nil {
		return err
	}

	methodsByUser := make(map[int64][]models.UserContactMethod, len(userIDs))
	for _, method := range methods {
		methodsByUser[method.UserID] = append(methodsByUser[method.UserID], method)
	}

	for _, match := range matches {
		userMessage := &models.UserMessage{
			MessageID:         message.ID,
			UserID:            match.UserID,
			MatchedLocationID: match.LocationID,
			ResolutionReason:  "location_match",
			SMSEnabled:        containsChannel(match.Channels, models.ChannelSMS),
			EmailEnabled:      containsChannel(match.Channels, models.ChannelEmail),
			VoiceEnabled:      containsChannel(match.Channels, models.ChannelVoice),
			Status:            "queued",
			QueuedAt:          d.now(),
		}
		if err := d.usersMessages.CreateOrGet(ctx, userMessage); err != nil {
			return err
		}

		notificationBacked := d.notifications != nil && message.NWSEventID != nil
		attemptsByChannel := make(map[models.Channel]*models.DeliveryAttempt, len(match.Channels))
		suppressedCount := 0

		if !notificationBacked {
			existingAttempts, err := d.deliveryAttempts.ListByUserMessageID(ctx, userMessage.ID)
			if err != nil {
				return err
			}
			for i := range existingAttempts {
				attempt := existingAttempts[i]
				attemptsByChannel[attempt.Channel] = &attempt
			}
		}

		for _, channel := range match.Channels {
			if notificationBacked {
				notification, createdNotification, err := d.ensureNotification(ctx, message, match.UserID, channel)
				if err != nil {
					return err
				}

				existingAttempts, err := d.deliveryAttempts.ListByNotificationID(ctx, notification.ID)
				if err != nil {
					return err
				}
				if attempt := latestAttempt(existingAttempts); attempt != nil {
					if notification.FirstMessageID != message.ID {
						attemptsByChannel[channel] = suppressedAttempt(channel)
						suppressedCount++
						continue
					}
					attemptsByChannel[channel] = attempt
					continue
				}

				attempt, err := d.dispatchChannelForMatch(ctx, message, match, methodsByUser[match.UserID], userMessage, notification, channel)
				if err != nil {
					return err
				}
				if attempt != nil {
					attemptsByChannel[channel] = attempt
				}

				if !createdNotification && notification.FirstMessageID != message.ID && attempt == nil {
					attemptsByChannel[channel] = suppressedAttempt(channel)
					suppressedCount++
				}
				continue
			}

			attempt := attemptsByChannel[channel]
			if attempt != nil && attempt.Status != "queued" {
				continue
			}

			attempt, err := d.dispatchChannelForMatch(ctx, message, match, methodsByUser[match.UserID], userMessage, nil, channel)
			if err != nil {
				return err
			}
			if attempt != nil {
				attemptsByChannel[channel] = attempt
			}
		}

		if len(match.Channels) > 0 && suppressedCount == len(match.Channels) {
			if err := d.usersMessages.UpdateStatus(ctx, userMessage.ID, "suppressed", nil); err != nil {
				return err
			}
			continue
		}

		status, deliveredAt := summarizeUserMessage(match.Channels, attemptsByChannel, d.now)
		if err := d.usersMessages.UpdateStatus(ctx, userMessage.ID, status, deliveredAt); err != nil {
			return err
		}
	}

	return nil
}

func (d *ChannelDispatcher) ensureNotification(ctx context.Context, message *models.Message, userID int64, channel models.Channel) (*models.Notification, bool, error) {
	notification := &models.Notification{
		NWSEventID:     *message.NWSEventID,
		UserID:         userID,
		Channel:        channel,
		FirstMessageID: message.ID,
		LastMessageID:  message.ID,
		Status:         "queued",
	}
	created, err := d.notifications.CreateOrGet(ctx, notification)
	if err != nil {
		return nil, false, err
	}
	if !created {
		if err := d.notifications.TouchLastMessage(ctx, notification.ID, message.ID); err != nil {
			return nil, false, err
		}
	}
	return notification, created, nil
}

func (d *ChannelDispatcher) dispatchChannelForMatch(ctx context.Context, message *models.Message, match UserMatch, methods []models.UserContactMethod, userMessage *models.UserMessage, notification *models.Notification, channel models.Channel) (*models.DeliveryAttempt, error) {
	attempt, created, err := d.ensureAttemptRecord(ctx, userMessage, notification, channel)
	if err != nil {
		return nil, err
	}
	if attempt == nil {
		return nil, nil
	}
	if !created {
		return attempt, nil
	}

	method, ok := selectContactMethod(methods, channel)
	if !ok {
		errMessage := "no active contact method"
		if err := d.deliveryAttempts.UpdateStatus(ctx, attempt.ID, "failed", nil, &errMessage, nil, nil); err != nil {
			return nil, err
		}
		attempt.Status = "failed"
		attempt.ErrorMessage = &errMessage
		if notification != nil {
			requestedAt := attempt.RequestedAt
			if err := d.notifications.UpdateStatus(ctx, notification.ID, "failed", message.ID, &requestedAt, nil, nil); err != nil {
				return nil, err
			}
		}
		return attempt, nil
	}

	attempt.Destination = method.Destination

	if channel == models.ChannelVoice {
		d.logf(
			"worker dispatching voice alert message_id=%d user_id=%d user_message_id=%d destination=%s",
			message.ID,
			match.UserID,
			userMessage.ID,
			method.Destination,
		)
	}

	result, sendErr := d.sendChannel(ctx, channel, method.Destination, message)
	if sendErr != nil {
		errMessage := sendErr.Error()
		if err := d.deliveryAttempts.UpdateStatus(ctx, attempt.ID, "failed", nil, &errMessage, nil, nil); err != nil {
			return nil, err
		}
		attempt.Status = "failed"
		attempt.ErrorMessage = &errMessage
		if notification != nil {
			requestedAt := attempt.RequestedAt
			if err := d.notifications.UpdateStatus(ctx, notification.ID, "failed", message.ID, &requestedAt, nil, nil); err != nil {
				return nil, err
			}
		}
		return attempt, nil
	}

	providerID := result.ProviderMessageID
	now := d.now()
	if err := d.deliveryAttempts.UpdateStatus(ctx, attempt.ID, "sent", &providerID, nil, &now, nil); err != nil {
		return nil, err
	}
	attempt.Status = "sent"
	attempt.Provider = optionalProvider(result.Provider)
	attempt.ProviderMessageID = &providerID
	attempt.ErrorMessage = nil
	attempt.SentAt = &now
	if notification != nil {
		requestedAt := attempt.RequestedAt
		if err := d.notifications.UpdateStatus(ctx, notification.ID, "sent", message.ID, &requestedAt, &now, nil); err != nil {
			return nil, err
		}
	}
	return attempt, nil
}

func (d *ChannelDispatcher) ensureAttemptRecord(ctx context.Context, userMessage *models.UserMessage, notification *models.Notification, channel models.Channel) (*models.DeliveryAttempt, bool, error) {
	attempt := &models.DeliveryAttempt{
		UserMessageID: userMessage.ID,
		Channel:       channel,
		AttemptNumber: 1,
		Destination:   "",
		Provider:      optionalProvider(providerName(channel)),
		Status:        "queued",
		RequestedAt:   d.now(),
	}
	if notification != nil {
		attempt.NotificationID = &notification.ID
	}

	if err := d.deliveryAttempts.Create(ctx, attempt); err != nil {
		if !sqlutil.IsDuplicateKey(err) {
			return nil, false, err
		}

		if notification != nil {
			existingAttempts, listErr := d.deliveryAttempts.ListByNotificationID(ctx, notification.ID)
			if listErr != nil {
				return nil, false, listErr
			}
			return latestAttempt(existingAttempts), false, nil
		}
		existing, getErr := d.deliveryAttempts.GetByUserMessageIDAndChannel(ctx, userMessage.ID, channel)
		return existing, false, getErr
	}

	return attempt, true, nil
}

func (d *ChannelDispatcher) sendChannel(ctx context.Context, channel models.Channel, destination string, message *models.Message) (deliveryResult, error) {
	if d.deliver == nil {
		return deliveryResult{}, fmt.Errorf("channel sender is not configured")
	}
	return d.deliver(ctx, channel, destination, message)
}

func (d *ChannelDispatcher) sendWithProviders(ctx context.Context, channel models.Channel, destination string, message *models.Message) (deliveryResult, error) {
	switch channel {
	case models.ChannelSMS:
		if d.twilio == nil {
			return deliveryResult{}, fmt.Errorf("twilio sms provider is not configured")
		}
		result, err := d.twilio.SendSMS(ctx, destination, message.Body)
		return deliveryResult{Provider: result.Provider, ProviderMessageID: result.ProviderMessageID, Status: result.Status}, err
	case models.ChannelVoice:
		if d.twilio == nil {
			return deliveryResult{}, fmt.Errorf("twilio voice provider is not configured")
		}
		result, err := d.twilio.SendVoice(ctx, destination, message.Body)
		return deliveryResult{Provider: result.Provider, ProviderMessageID: result.ProviderMessageID, Status: result.Status}, err
	case models.ChannelEmail:
		if d.sendgrid == nil {
			return deliveryResult{}, fmt.Errorf("sendgrid provider is not configured")
		}
		subject := strings.TrimSpace(stringValue(message.Title))
		if subject == "" {
			subject = message.MessageType
		}
		result, err := d.sendgrid.SendEmail(ctx, destination, subject, message.Body)
		return deliveryResult{Provider: result.Provider, ProviderMessageID: result.ProviderMessageID, Status: result.Status}, err
	default:
		return deliveryResult{}, fmt.Errorf("unsupported delivery channel %q", channel)
	}
}

func selectContactMethod(methods []models.UserContactMethod, channel models.Channel) (models.UserContactMethod, bool) {
	fallbacks := []models.Channel{channel}
	if channel == models.ChannelVoice {
		fallbacks = append(fallbacks, models.ChannelSMS)
	}

	for _, fallback := range fallbacks {
		for _, method := range methods {
			if method.Active && method.Channel == fallback {
				return method, true
			}
		}
	}

	return models.UserContactMethod{}, false
}

func optionalProvider(provider string) *string {
	if strings.TrimSpace(provider) == "" {
		return nil
	}
	return &provider
}

func providerName(channel models.Channel) string {
	switch channel {
	case models.ChannelEmail:
		return "sendgrid_email"
	case models.ChannelSMS:
		return "twilio_sms"
	case models.ChannelVoice:
		return "twilio_voice"
	default:
		return ""
	}
}

func summarizeUserMessage(channels []models.Channel, attemptsByChannel map[models.Channel]*models.DeliveryAttempt, now func() time.Time) (string, *time.Time) {
	successCount := 0
	failureCount := 0
	pendingCount := 0
	var deliveredAt *time.Time

	for _, channel := range channels {
		attempt := attemptsByChannel[channel]
		if attempt == nil {
			pendingCount++
			continue
		}

		switch attempt.Status {
		case "sent":
			successCount++
			if attempt.SentAt != nil && (deliveredAt == nil || attempt.SentAt.After(*deliveredAt)) {
				sentAt := *attempt.SentAt
				deliveredAt = &sentAt
			}
		case "failed":
			failureCount++
		case "suppressed":
			continue
		default:
			pendingCount++
		}
	}

	if pendingCount > 0 {
		return "queued", nil
	}
	if successCount > 0 && failureCount == 0 {
		if deliveredAt == nil {
			sentAt := now()
			deliveredAt = &sentAt
		}
		return "sent", deliveredAt
	}
	if successCount > 0 && failureCount > 0 {
		return "partial_failure", nil
	}
	if failureCount > 0 {
		return "failed", nil
	}
	return "skipped", nil
}

type deliveryResult struct {
	Provider          string
	ProviderMessageID string
	Status            string
}

func latestAttempt(attempts []models.DeliveryAttempt) *models.DeliveryAttempt {
	if len(attempts) == 0 {
		return nil
	}

	latest := attempts[0]
	for i := 1; i < len(attempts); i++ {
		current := attempts[i]
		if current.AttemptNumber > latest.AttemptNumber || (current.AttemptNumber == latest.AttemptNumber && current.ID > latest.ID) {
			latest = current
		}
	}
	return &latest
}

func suppressedAttempt(channel models.Channel) *models.DeliveryAttempt {
	return &models.DeliveryAttempt{
		Channel: channel,
		Status:  "suppressed",
	}
}

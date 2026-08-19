package voicedispatcher

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"thundercall-go/internal/logging"
	twilioprovider "thundercall-go/internal/providers/twilio"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
)

type attemptsRepository interface {
	GetLatestSentVoiceAttemptByMessageID(ctx context.Context, messageID int64) (*deliveryattemptsrepo.VoiceDispatchRecord, error)
	UpdateStatus(ctx context.Context, id int64, status string, providerMessageID *string, errorMessage *string, sentAt *time.Time, deliveredAt *time.Time) error
	Requeue(ctx context.Context, id int64, errorMessage *string, dispatchAfter time.Time) error
}

type usersMessagesRepository interface {
	UpdateStatus(ctx context.Context, id int64, status string, deliveredAt *time.Time) error
}

type notificationsRepository interface {
	UpdateStatus(ctx context.Context, id int64, status string, lastMessageID int64, firstAttemptedAt *time.Time, sentAt *time.Time, deliveredAt *time.Time) error
}

type voiceSender interface {
	SendVoice(ctx context.Context, request twilioprovider.VoiceRequest) (twilioprovider.Result, error)
	ResolveVoiceDestination(to string) (string, bool)
	BuildTestVoiceBody(intendedTo string, body string) string
	BuildCollapsedTestVoiceBody(intendedTo string, body string) string
	CollapseVoiceOverrideCalls() bool
	VoiceFrom() string
}

type Service struct {
	attempts      attemptsRepository
	usersMessages usersMessagesRepository
	notifications notificationsRepository
	sender        voiceSender
	retryDelay    time.Duration
	waiter        turnWaiter
	now           func() time.Time
	infof         func(string, ...any)
	warnf         func(string, ...any)
	debugf        func(string, ...any)
	cpsLimit      int

	mu                 sync.Mutex
	collapsedByMessage map[int64]dispatchOutcome
	recentDispatches   []time.Time
	messageDispatches  map[int64]int
}

type dispatchOutcome struct {
	providerMessageID string
	delivered         bool
}

func NewService(
	attempts attemptsRepository,
	usersMessages usersMessagesRepository,
	notifications notificationsRepository,
	sender voiceSender,
	waiter turnWaiter,
	retryDelay time.Duration,
) *Service {
	if retryDelay <= 0 {
		retryDelay = 30 * time.Second
	}

	logger := logging.New("voice-dispatcher")
	return &Service{
		attempts:           attempts,
		usersMessages:      usersMessages,
		notifications:      notifications,
		sender:             sender,
		retryDelay:         retryDelay,
		waiter:             waiter,
		now:                func() time.Time { return time.Now().UTC() },
		infof:              logger.Infof,
		warnf:              logger.Warnf,
		debugf:             logger.Debugf,
		collapsedByMessage: make(map[int64]dispatchOutcome),
		messageDispatches:  make(map[int64]int),
	}
}

func (s *Service) SetCallsPerSecond(limit int) {
	if limit < 0 {
		limit = 0
	}
	s.cpsLimit = limit
}

func (s *Service) ProcessAttempt(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord) error {
	if s.attempts == nil {
		return fmt.Errorf("delivery attempts repository is required")
	}
	if s.usersMessages == nil {
		return fmt.Errorf("users_messages repository is required")
	}
	if s.sender == nil {
		return fmt.Errorf("voice sender is required")
	}

	if outcome, ok, err := s.collapsedProviderMessageID(ctx, record); err != nil {
		return err
	} else if ok {
		return s.markProviderResult(ctx, record, outcome.providerMessageID, providerResultStatus(outcome.delivered), false)
	}

	sendTo := record.Attempt.Destination
	body := record.MessageBody
	if overrideDestination, overridden := s.sender.ResolveVoiceDestination(record.Attempt.Destination); overridden {
		sendTo = overrideDestination
		if s.sender.CollapseVoiceOverrideCalls() {
			body = s.sender.BuildCollapsedTestVoiceBody(record.Attempt.Destination, record.MessageBody)
		} else {
			body = s.sender.BuildTestVoiceBody(record.Attempt.Destination, record.MessageBody)
		}
	}

	if s.waiter != nil {
		if err := s.waiter.Wait(ctx); err != nil {
			return err
		}
	}

	result, err := s.sender.SendVoice(ctx, twilioprovider.VoiceRequest{
		To:            sendTo,
		Body:          body,
		EventCode:     record.EventCode,
		AlertTypeCode: record.AlertTypeCode,
		AccountID:     int64Value(record.AccountID),
	})
	persistCtx := context.WithoutCancel(ctx)
	if err != nil {
		return s.handleSendError(persistCtx, record, sendTo, err)
	}

	if err := s.markProviderResult(persistCtx, record, result.ProviderMessageID, result.Status, true); err != nil {
		return err
	}

	dispatchedAt := s.now()
	currentCPS, messageDispatchCount := s.recordDispatch(record.MessageID, dispatchedAt)
	if s.infof != nil {
		s.infof(
			"event=voice_call_sent message_id=%d user_id=%d attempt_id=%d provider_message_id=%s from=%s intended_to=%s send_to=%s cps_current=%.2f cps_limit=%d message_dispatch_count=%d",
			record.MessageID,
			record.UserID,
			record.Attempt.ID,
			blankDash(result.ProviderMessageID),
			blankDash(s.sender.VoiceFrom()),
			blankDash(record.Attempt.Destination),
			blankDash(sendTo),
			currentCPS,
			s.cpsLimit,
			messageDispatchCount,
		)
	}
	return nil
}

func (s *Service) collapsedProviderMessageID(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord) (dispatchOutcome, bool, error) {
	if !s.sender.CollapseVoiceOverrideCalls() || record.MessageID <= 0 {
		return dispatchOutcome{}, false, nil
	}

	s.mu.Lock()
	cached, ok := s.collapsedByMessage[record.MessageID]
	s.mu.Unlock()
	if ok && strings.TrimSpace(cached.providerMessageID) != "" {
		return cached, true, nil
	}

	existing, err := s.attempts.GetLatestSentVoiceAttemptByMessageID(ctx, record.MessageID)
	if err != nil {
		return dispatchOutcome{}, false, err
	}
	if existing == nil || existing.Attempt.ProviderMessageID == nil || strings.TrimSpace(*existing.Attempt.ProviderMessageID) == "" {
		return dispatchOutcome{}, false, nil
	}

	outcome := dispatchOutcome{
		providerMessageID: strings.TrimSpace(*existing.Attempt.ProviderMessageID),
		delivered:         existing.Attempt.DeliveredAt != nil,
	}
	s.mu.Lock()
	s.collapsedByMessage[record.MessageID] = outcome
	s.mu.Unlock()
	return outcome, true, nil
}

func (s *Service) handleSendError(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord, sendTo string, sendErr error) error {
	errMessage := sendErr.Error()
	if twilioprovider.IsRetryableError(sendErr) {
		dispatchAfter := s.now().Add(s.retryDelay)
		if err := s.attempts.Requeue(ctx, record.Attempt.ID, &errMessage, dispatchAfter); err != nil {
			return err
		}
		if record.Attempt.NotificationID != nil && s.notifications != nil {
			requestedAt := record.Attempt.RequestedAt
			if err := s.notifications.UpdateStatus(ctx, *record.Attempt.NotificationID, "queued", record.MessageID, &requestedAt, nil, nil); err != nil {
				return err
			}
		}
		if s.warnf != nil {
			s.warnf(
				"event=voice_call_retry attempt_id=%d message_id=%d user_id=%d from=%s intended_to=%s send_to=%s dispatch_after=%s error=%q",
				record.Attempt.ID,
				record.MessageID,
				record.UserID,
				blankDash(s.sender.VoiceFrom()),
				blankDash(record.Attempt.Destination),
				blankDash(sendTo),
				dispatchAfter.Format(time.RFC3339),
				sendErr,
			)
		}
		return nil
	}

	if err := s.attempts.UpdateStatus(ctx, record.Attempt.ID, "failed", nil, &errMessage, nil, nil); err != nil {
		return err
	}
	if err := s.usersMessages.UpdateStatus(ctx, record.Attempt.UserMessageID, "failed", nil); err != nil {
		return err
	}
	if record.Attempt.NotificationID != nil && s.notifications != nil {
		requestedAt := record.Attempt.RequestedAt
		if err := s.notifications.UpdateStatus(ctx, *record.Attempt.NotificationID, "failed", record.MessageID, &requestedAt, nil, nil); err != nil {
			return err
		}
	}
	if s.warnf != nil {
		s.warnf(
			"event=voice_call_failed attempt_id=%d message_id=%d user_id=%d from=%s intended_to=%s send_to=%s error=%q",
			record.Attempt.ID,
			record.MessageID,
			record.UserID,
			blankDash(s.sender.VoiceFrom()),
			blankDash(record.Attempt.Destination),
			blankDash(sendTo),
			sendErr,
		)
	}
	return nil
}

func (s *Service) markProviderResult(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord, providerMessageID string, providerStatus string, cacheCollapsed bool) error {
	if providerResultDelivered(providerStatus) {
		return s.markDelivered(ctx, record, providerMessageID, cacheCollapsed)
	}
	return s.markSent(ctx, record, providerMessageID, cacheCollapsed)
}

func (s *Service) markSent(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord, providerMessageID string, cacheCollapsed bool) error {
	now := s.now()
	if err := s.attempts.UpdateStatus(ctx, record.Attempt.ID, "sent", &providerMessageID, nil, &now, nil); err != nil {
		return err
	}
	if err := s.usersMessages.UpdateStatus(ctx, record.Attempt.UserMessageID, "sent", &now); err != nil {
		return err
	}
	if record.Attempt.NotificationID != nil && s.notifications != nil {
		requestedAt := record.Attempt.RequestedAt
		if err := s.notifications.UpdateStatus(ctx, *record.Attempt.NotificationID, "sent", record.MessageID, &requestedAt, &now, nil); err != nil {
			return err
		}
	}

	if cacheCollapsed && s.sender.CollapseVoiceOverrideCalls() && record.MessageID > 0 && strings.TrimSpace(providerMessageID) != "" {
		s.mu.Lock()
		s.collapsedByMessage[record.MessageID] = dispatchOutcome{providerMessageID: providerMessageID}
		s.mu.Unlock()
	}

	return nil
}

func (s *Service) markDelivered(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord, providerMessageID string, cacheCollapsed bool) error {
	now := s.now()
	if err := s.attempts.UpdateStatus(ctx, record.Attempt.ID, "sent", &providerMessageID, nil, &now, &now); err != nil {
		return err
	}
	if err := s.usersMessages.UpdateStatus(ctx, record.Attempt.UserMessageID, "sent", &now); err != nil {
		return err
	}
	if record.Attempt.NotificationID != nil && s.notifications != nil {
		requestedAt := record.Attempt.RequestedAt
		if err := s.notifications.UpdateStatus(ctx, *record.Attempt.NotificationID, "sent", record.MessageID, &requestedAt, &now, &now); err != nil {
			return err
		}
	}

	if cacheCollapsed && s.sender.CollapseVoiceOverrideCalls() && record.MessageID > 0 && strings.TrimSpace(providerMessageID) != "" {
		s.mu.Lock()
		s.collapsedByMessage[record.MessageID] = dispatchOutcome{
			providerMessageID: providerMessageID,
			delivered:         true,
		}
		s.mu.Unlock()
	}

	return nil
}

func providerResultDelivered(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "delivered":
		return true
	default:
		return false
	}
}

func providerResultStatus(delivered bool) string {
	if delivered {
		return "completed"
	}
	return "sent"
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *Service) recordDispatch(messageID int64, at time.Time) (float64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := at.Add(-1 * time.Second)
	kept := s.recentDispatches[:0]
	for _, seenAt := range s.recentDispatches {
		if !seenAt.Before(cutoff) {
			kept = append(kept, seenAt)
		}
	}
	s.recentDispatches = append(kept, at)

	s.messageDispatches[messageID]++
	return float64(len(s.recentDispatches)), s.messageDispatches[messageID]
}

func blankDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

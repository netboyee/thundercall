package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"thundercall-go/internal/logging"
	messagesrepo "thundercall-go/internal/repositories/messages"
	"thundercall-go/internal/thundercall"
)

var ErrMessageNotFound = errors.New("message not found")

type Service struct {
	messages   *messagesrepo.Repository
	resolver   thundercall.RecipientResolver
	dispatcher thundercall.Dispatcher
	now        func() time.Time
	logf       func(string, ...any)
}

func NewService(messages *messagesrepo.Repository, resolver thundercall.RecipientResolver, dispatcher thundercall.Dispatcher) *Service {
	logger := logging.New("worker")
	return &Service{
		messages:   messages,
		resolver:   resolver,
		dispatcher: dispatcher,
		now:        func() time.Time { return time.Now().UTC() },
		logf:       logger.Infof,
	}
}

func (s *Service) ProcessMessage(ctx context.Context, messageID int64) error {
	if s.messages == nil {
		return fmt.Errorf("messages repository is required")
	}

	message, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return fmt.Errorf("get message %d: %w", messageID, err)
	}
	if message == nil {
		return fmt.Errorf("%w: %d", ErrMessageNotFound, messageID)
	}

	var matches []thundercall.UserMatch
	if s.resolver != nil {
		matches, err = s.resolver.ResolveRecipients(ctx, message)
		if err != nil {
			return fmt.Errorf("resolve recipients for message %d: %w", messageID, err)
		}
	}
	if s.logf != nil {
		s.logf(
			"event=recipient_resolution message_id=%d source_message_id=%d event_code=%s action=%s alert_type=%s matched_users=%d polygon=%t fips=%d zones=%d",
			message.ID,
			int64Value(message.SourceMessageID),
			message.EventCode,
			dashString(message.VTECAction),
			message.AlertTypeCode,
			len(matches),
			stringValue(message.PolygonWKT) != "",
			len(message.FIPSCodes),
			len(message.NWSZones),
		)
	}

	if s.dispatcher != nil {
		if err := s.dispatcher.Dispatch(ctx, message, matches); err != nil {
			return fmt.Errorf("dispatch message %d: %w", messageID, err)
		}
	}

	processedAt := s.now()
	if err := s.messages.UpdateStatus(ctx, message.ID, "processed", &processedAt); err != nil {
		return fmt.Errorf("mark message %d processed: %w", messageID, err)
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func dashString(value *string) string {
	if value == nil || *value == "" {
		return "-"
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

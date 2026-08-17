package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"thundercall-go/internal/events"
	"thundercall-go/internal/models"
	"thundercall-go/internal/nwws"
	messagesrepo "thundercall-go/internal/repositories/messages"
	nwseventsrepo "thundercall-go/internal/repositories/nwsevents"
	outboxeventsrepo "thundercall-go/internal/repositories/outboxevents"
	sourcemessagesrepo "thundercall-go/internal/repositories/sourcemessages"
	"thundercall-go/internal/repositories/sqlutil"
	"thundercall-go/internal/thundercall"
)

type ProcessResult struct {
	SourceMessageID int64
	MessageIDs      []int64
	AcceptedCount   int
	IgnoredCount    int
	Duplicate       bool
}

type Service struct {
	db              *sql.DB
	parser          *nwws.Parser
	streamKey       string
	allowedProducts map[string]struct{}
	now             func() time.Time
}

func NewService(db *sql.DB, streamKey string, allowedProducts []string) *Service {
	normalized := make(map[string]struct{}, len(allowedProducts))
	for _, product := range allowedProducts {
		product = strings.ToUpper(strings.TrimSpace(product))
		if product != "" {
			normalized[product] = struct{}{}
		}
	}

	return &Service{
		db:              db,
		parser:          nwws.NewParser(),
		streamKey:       streamKey,
		allowedProducts: normalized,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) ProcessEnvelope(ctx context.Context, envelope nwws.StanzaEnvelope) (ProcessResult, error) {
	envelopeCategory := s.productCategory(envelope.AWIPSID)
	parsed, err := s.parser.Parse(envelope.Body, envelope.IssueTime)
	parsedCategory := strings.ToUpper(strings.TrimSpace(parsed.AWIPSIdentifier.ProductCategory))
	effectiveCategory := firstNonEmpty(parsedCategory, envelopeCategory)

	if err != nil {
		if effectiveCategory == "" || !s.productAllowed(effectiveCategory) {
			return ProcessResult{IgnoredCount: 1}, nil
		}
	}

	externalID := s.externalID(envelope)
	if err == nil {
		previewRequests := nwws.Normalize(parsed, 0, externalID)
		loadableCount := 0
		ignoredCount := 0
		for _, req := range previewRequests {
			if !s.requestAllowed(req, effectiveCategory) {
				ignoredCount++
				continue
			}
			if !thundercall.ShouldLoadNWWSMessage(req) {
				ignoredCount++
				continue
			}
			if err := req.Validate(); err != nil {
				return ProcessResult{}, fmt.Errorf("normalized NWWS message validation failed: %w", err)
			}
			loadableCount++
		}
		if loadableCount == 0 {
			if ignoredCount == 0 {
				ignoredCount = 1
			}
			return ProcessResult{IgnoredCount: ignoredCount}, nil
		}
	}

	if s.db == nil {
		return ProcessResult{}, fmt.Errorf("database is required")
	}

	sourceRepo := sourcemessagesrepo.New(s.db)

	existing, err := sourceRepo.GetBySourceAndExternalID(ctx, "NWWS", externalID)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("lookup source message %s: %w", externalID, err)
	}
	if existing != nil {
		return ProcessResult{
			SourceMessageID: existing.ID,
			Duplicate:       true,
		}, nil
	}

	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("begin ingest transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	txSourceRepo := sourcemessagesrepo.NewWithDBTX(tx)
	txMessagesRepo := messagesrepo.NewWithDBTX(tx)
	txEventsRepo := nwseventsrepo.NewWithDBTX(tx)
	txOutboxRepo := outboxeventsrepo.NewWithDBTX(tx)

	sourceMessage := &models.SourceMessage{
		Source:          "NWWS",
		ExternalID:      externalID,
		WMOCode:         nullableString(strings.ToUpper(strings.TrimSpace(envelope.WMOCode))),
		WFOCode:         nullableString(strings.ToUpper(strings.TrimSpace(envelope.CCCCode))),
		AWIPSID:         nullableString(strings.ToUpper(strings.TrimSpace(envelope.AWIPSID))),
		ProductCategory: nullableString(effectiveCategory),
		IssuedAt:        timePointer(envelope.IssueTime),
		RawPayload:      envelope.Body,
		Status:          "received",
		ReceivedAt:      now,
	}
	if err := txSourceRepo.Create(ctx, sourceMessage); err != nil {
		if sqlutil.IsDuplicateKey(err) {
			existing, lookupErr := sourceRepo.GetBySourceAndExternalID(ctx, "NWWS", externalID)
			if lookupErr != nil {
				return ProcessResult{}, fmt.Errorf("lookup duplicate source message %s: %w", externalID, lookupErr)
			}
			if existing != nil {
				return ProcessResult{
					SourceMessageID: existing.ID,
					Duplicate:       true,
				}, nil
			}
		}
		return ProcessResult{}, fmt.Errorf("create source message %s: %w", externalID, err)
	}

	result := ProcessResult{SourceMessageID: sourceMessage.ID}
	if err != nil {
		parseError := err.Error()
		parsedAt := now
		if updateErr := txSourceRepo.UpdateStatus(ctx, sourceMessage.ID, "parse_failed", &parseError, &parsedAt); updateErr != nil {
			return ProcessResult{}, fmt.Errorf("mark source message %d parse_failed after parse error %q: %w", sourceMessage.ID, parseError, updateErr)
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return ProcessResult{}, fmt.Errorf("commit parse_failed source message %d: %w", sourceMessage.ID, commitErr)
		}
		return result, nil
	}

	requests := nwws.Normalize(parsed, sourceMessage.ID, externalID)
	for _, req := range requests {
		if !s.requestAllowed(req, effectiveCategory) {
			result.IgnoredCount++
			continue
		}
		if !thundercall.ShouldLoadNWWSMessage(req) {
			result.IgnoredCount++
			continue
		}
		if err := req.Validate(); err != nil {
			return ProcessResult{}, fmt.Errorf("normalized NWWS message validation failed: %w", err)
		}

		resolvedEvent, err := resolveNWSEvent(ctx, txEventsRepo, req)
		if err != nil {
			return ProcessResult{}, fmt.Errorf("resolve NWWS event for source %d segment %d: %w", sourceMessage.ID, req.SourceSegmentIndex, err)
		}

		message := &models.Message{
			Source:          req.MessageSource,
			EventCode:       req.MessageEvent,
			MessageType:     req.MessageType,
			AlertTypeCode:   thundercall.AlertTypeFromEvent(req.AlertEventCode()),
			Title:           nullableString(req.Title),
			Body:            req.Body,
			Coordinate:      nullableString(req.Coordinate),
			PolygonWKT:      nullableString(req.Polygon),
			FIPSCodes:       models.StringSlice(append([]string(nil), req.FIPSCodes...)),
			NWSZones:        models.StringSlice(append([]string(nil), req.NWSZones...)),
			PrimaryVTECRaw:  nullableString(req.PrimaryVTECRaw),
			VTECAction:      nullableString(req.VTECAction),
			OriginalPayload: nullableString(req.Original),
			Fingerprint:     thundercall.GenerateFingerprint(req.MessageType, req.Polygon, req.FIPSCodes, req.NWSZones),
			Status:          "accepted",
			IssuedAt:        timePointer(req.Timestamp),
			ReceivedAt:      now,
		}
		if resolvedEvent != nil {
			message.NWSEventID = &resolvedEvent.ID
		}
		if req.SourceMessageID > 0 {
			message.SourceMessageID = &req.SourceMessageID
			message.SourceSegmentIndex = &req.SourceSegmentIndex
		}
		if strings.TrimSpace(req.ExternalID) != "" {
			message.ExternalMessageID = &req.ExternalID
		}

		if err := txMessagesRepo.Create(ctx, message); err != nil {
			if sqlutil.IsDuplicateKey(err) {
				continue
			}
			return ProcessResult{}, fmt.Errorf("create message for source %d segment %d: %w", sourceMessage.ID, req.SourceSegmentIndex, err)
		}

		payload, err := events.EncodeMessageAccepted(message.ID)
		if err != nil {
			return ProcessResult{}, fmt.Errorf("encode outbox payload for message %d: %w", message.ID, err)
		}

		if err := txOutboxRepo.Create(ctx, &models.OutboxEvent{
			AggregateType: "message",
			AggregateID:   message.ID,
			EventType:     events.EventTypeMessageAccepted,
			StreamKey:     s.streamKey,
			PayloadJSON:   payload,
		}); err != nil {
			return ProcessResult{}, fmt.Errorf("create outbox event for message %d: %w", message.ID, err)
		}

		result.MessageIDs = append(result.MessageIDs, message.ID)
		result.AcceptedCount++
	}

	status := "parsed"
	if result.AcceptedCount == 0 && result.IgnoredCount > 0 {
		status = "ignored"
	}
	parsedAt := now
	if err := txSourceRepo.UpdateStatus(ctx, sourceMessage.ID, status, nil, &parsedAt); err != nil {
		return ProcessResult{}, fmt.Errorf("mark source message %d %s: %w", sourceMessage.ID, status, err)
	}

	if err := tx.Commit(); err != nil {
		return ProcessResult{}, fmt.Errorf("commit ingest for source %d: %w", sourceMessage.ID, err)
	}
	return result, nil
}

func (s *Service) productAllowed(product string) bool {
	if len(s.allowedProducts) == 0 {
		return true
	}
	_, ok := s.allowedProducts[strings.ToUpper(strings.TrimSpace(product))]
	return ok
}

func (s *Service) requestAllowed(req thundercall.IncomingMessageRequest, fallbackProducts ...string) bool {
	if len(s.allowedProducts) == 0 {
		return true
	}

	if s.productAllowed(req.ConfiguredProductCode()) {
		return true
	}

	for _, product := range fallbackProducts {
		if s.productAllowed(product) {
			return true
		}
	}

	return false
}

func (s *Service) skipUnconfiguredProduct(product string) bool {
	product = strings.ToUpper(strings.TrimSpace(product))
	if product == "" {
		return false
	}
	return !s.productAllowed(product)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Service) productCategory(awipsID string) string {
	awipsID = strings.ToUpper(strings.TrimSpace(awipsID))
	if len(awipsID) < 3 {
		return awipsID
	}
	return awipsID[:3]
}

func (s *Service) externalID(envelope nwws.StanzaEnvelope) string {
	if value := strings.TrimSpace(envelope.ExternalID); value != "" {
		return value
	}

	hash := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(envelope.WMOCode),
		strings.TrimSpace(envelope.AWIPSID),
		envelope.IssueTime.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(envelope.Body),
	}, "|")))
	return hex.EncodeToString(hash[:])
}

func nullableString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

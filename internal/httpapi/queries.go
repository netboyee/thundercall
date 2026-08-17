package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"thundercall-go/internal/models"
	"thundercall-go/internal/repositories/sqlutil"
)

type messageListFilter struct {
	From        *time.Time
	To          *time.Time
	Search      string
	EventCode   string
	MessageType string
	Status      string
	Source      string
	Limit       int
	Offset      int
}

type locationListFilter struct {
	Search     string
	ActiveOnly *bool
	Limit      int
	Offset     int
}

type deliveryListFilter struct {
	Search string
	Status string
	Limit  int
	Offset int
}

type pagination struct {
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

type messageCounts struct {
	RecipientsCount               int64 `json:"recipientsCount"`
	AttemptsCount                 int64 `json:"attemptsCount"`
	SentRecipientsCount           int64 `json:"sentRecipientsCount"`
	FailedRecipientsCount         int64 `json:"failedRecipientsCount"`
	PartialFailureRecipientsCount int64 `json:"partialFailureRecipientsCount"`
	SMSAttemptsCount              int64 `json:"smsAttemptsCount"`
	EmailAttemptsCount            int64 `json:"emailAttemptsCount"`
	VoiceAttemptsCount            int64 `json:"voiceAttemptsCount"`
	SMSSentCount                  int64 `json:"smsSentCount"`
	EmailSentCount                int64 `json:"emailSentCount"`
	VoiceSentCount                int64 `json:"voiceSentCount"`
}

type dashboardSummary struct {
	MessagesCount int64 `json:"messagesCount"`
	messageCounts
}

type messageListItem struct {
	ID                 int64         `json:"id"`
	Source             string        `json:"source"`
	EventCode          string        `json:"eventCode"`
	MessageType        string        `json:"messageType"`
	AlertTypeCode      string        `json:"alertTypeCode"`
	Title              *string       `json:"title,omitempty"`
	Status             string        `json:"status"`
	IssuedAt           *time.Time    `json:"issuedAt,omitempty"`
	ReceivedAt         time.Time     `json:"receivedAt"`
	ProcessedAt        *time.Time    `json:"processedAt,omitempty"`
	SourceMessageID    *int64        `json:"sourceMessageId,omitempty"`
	ExternalMessageID  *string       `json:"externalMessageId,omitempty"`
	SourceSegmentIndex *int          `json:"sourceSegmentIndex,omitempty"`
	PolygonWKT         *string       `json:"polygonWKT,omitempty"`
	FIPSCodes          []string      `json:"fipsCodes,omitempty"`
	NWSZones           []string      `json:"nwsZones,omitempty"`
	Counts             messageCounts `json:"counts"`
}

type locationListItem struct {
	ID                   int64    `json:"id"`
	Name                 string   `json:"name"`
	AddressLine1         *string  `json:"addressLine1,omitempty"`
	AddressLine2         *string  `json:"addressLine2,omitempty"`
	City                 *string  `json:"city,omitempty"`
	StateCode            *string  `json:"stateCode,omitempty"`
	PostalCode           *string  `json:"postalCode,omitempty"`
	CountyFIPS           *string  `json:"countyFips,omitempty"`
	NWSZone              *string  `json:"nwsZone,omitempty"`
	Latitude             *float64 `json:"latitude,omitempty"`
	Longitude            *float64 `json:"longitude,omitempty"`
	CoverageWKT          *string  `json:"coverageWKT,omitempty"`
	IsThunderCallEnabled bool     `json:"isThunderCallEnabled"`
	Active               bool     `json:"active"`
	SubscribedUsersCount int64    `json:"subscribedUsersCount"`
}

type messageLocationItem struct {
	locationListItem
	MatchedUsersCount int64 `json:"matchedUsersCount"`
	SMSEnabledCount   int64 `json:"smsEnabledCount"`
	EmailEnabledCount int64 `json:"emailEnabledCount"`
	VoiceEnabledCount int64 `json:"voiceEnabledCount"`
}

type deliveryAttemptItem struct {
	ID                int64      `json:"id"`
	Channel           string     `json:"channel"`
	Destination       string     `json:"destination"`
	Provider          *string    `json:"provider,omitempty"`
	ProviderMessageID *string    `json:"providerMessageId,omitempty"`
	Status            string     `json:"status"`
	ErrorMessage      *string    `json:"errorMessage,omitempty"`
	RequestedAt       time.Time  `json:"requestedAt"`
	SentAt            *time.Time `json:"sentAt,omitempty"`
	DeliveredAt       *time.Time `json:"deliveredAt,omitempty"`
}

type messageDeliveryItem struct {
	UserMessageID   int64                 `json:"userMessageId"`
	UserID          int64                 `json:"userId"`
	DisplayName     string                `json:"displayName"`
	Title           *string               `json:"title,omitempty"`
	Status          string                `json:"status"`
	QueuedAt        time.Time             `json:"queuedAt"`
	DeliveredAt     *time.Time            `json:"deliveredAt,omitempty"`
	SMSEnabled      bool                  `json:"smsEnabled"`
	EmailEnabled    bool                  `json:"emailEnabled"`
	VoiceEnabled    bool                  `json:"voiceEnabled"`
	MatchedLocation *matchedLocationInfo  `json:"matchedLocation,omitempty"`
	Attempts        []deliveryAttemptItem `json:"attempts"`
}

type matchedLocationInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (s *Server) listMessages(ctx context.Context, accountID int64, filter messageListFilter) ([]messageListItem, int64, error) {
	whereSQL, whereArgs := buildMessageWhereClause(accountID, filter)

	countQuery := `SELECT COUNT(*) FROM messages m WHERE ` + whereSQL
	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery, whereArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT
			m.id,
			m.source,
			m.event_code,
			m.message_type,
			m.alert_type_code,
			m.title,
			m.status,
			m.issued_at,
			m.received_at,
			m.processed_at,
			m.source_message_id,
			m.external_message_id,
			m.source_segment_index,
			m.polygon_wkt,
			m.fips_codes_json,
			m.nws_zones_json,
			COALESCE(ums.recipients_count, 0),
			COALESCE(ums.sent_recipients_count, 0),
			COALESCE(ums.failed_recipients_count, 0),
			COALESCE(ums.partial_failure_recipients_count, 0),
			COALESCE(das.attempts_count, 0),
			COALESCE(das.sms_attempts_count, 0),
			COALESCE(das.email_attempts_count, 0),
			COALESCE(das.voice_attempts_count, 0),
			COALESCE(das.sms_sent_count, 0),
			COALESCE(das.email_sent_count, 0),
			COALESCE(das.voice_sent_count, 0)
		FROM messages m
		LEFT JOIN (
			SELECT
				um.message_id,
				COUNT(*) AS recipients_count,
				SUM(CASE WHEN um.status = 'sent' THEN 1 ELSE 0 END) AS sent_recipients_count,
				SUM(CASE WHEN um.status = 'failed' THEN 1 ELSE 0 END) AS failed_recipients_count,
				SUM(CASE WHEN um.status = 'partial_failure' THEN 1 ELSE 0 END) AS partial_failure_recipients_count
			FROM users_messages um
			INNER JOIN users u
				ON u.id = um.user_id
			WHERE u.account_id = ?
			GROUP BY um.message_id
		) ums
			ON ums.message_id = m.id
		LEFT JOIN (
			SELECT
				um.message_id,
				COUNT(da.id) AS attempts_count,
				SUM(CASE WHEN da.channel = 'sms' THEN 1 ELSE 0 END) AS sms_attempts_count,
				SUM(CASE WHEN da.channel = 'email' THEN 1 ELSE 0 END) AS email_attempts_count,
				SUM(CASE WHEN da.channel = 'voice' THEN 1 ELSE 0 END) AS voice_attempts_count,
				SUM(CASE WHEN da.channel = 'sms' AND da.status = 'sent' THEN 1 ELSE 0 END) AS sms_sent_count,
				SUM(CASE WHEN da.channel = 'email' AND da.status = 'sent' THEN 1 ELSE 0 END) AS email_sent_count,
				SUM(CASE WHEN da.channel = 'voice' AND da.status = 'sent' THEN 1 ELSE 0 END) AS voice_sent_count
			FROM users_messages um
			INNER JOIN users u
				ON u.id = um.user_id
			LEFT JOIN delivery_attempts da
				ON da.users_message_id = um.id
			WHERE u.account_id = ?
			GROUP BY um.message_id
		) das
			ON das.message_id = m.id
		WHERE ` + whereSQL + `
		ORDER BY COALESCE(m.issued_at, m.received_at) DESC, m.id DESC
		LIMIT ? OFFSET ?`

	args := make([]any, 0, 2+len(whereArgs)+2)
	args = append(args, accountID, accountID)
	args = append(args, whereArgs...)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]messageListItem, 0, filter.Limit)
	for rows.Next() {
		item, err := scanMessageListItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}

func (s *Server) getMessageDetail(ctx context.Context, accountID int64, messageID int64) (*messageListItem, error) {
	query := `
		SELECT
			id,
			source,
			event_code,
			message_type,
			alert_type_code,
			title,
			status,
			issued_at,
			received_at,
			processed_at,
			source_message_id,
			external_message_id,
			source_segment_index,
			polygon_wkt,
			fips_codes_json,
			nws_zones_json
		FROM messages
		WHERE id = ?
		  AND EXISTS (
			SELECT 1
			FROM users_messages um
			INNER JOIN users u
				ON u.id = um.user_id
			WHERE um.message_id = messages.id
			  AND u.account_id = ?
		  )`

	var (
		item               messageListItem
		title              sql.NullString
		issuedAt           sql.NullTime
		processedAt        sql.NullTime
		sourceMessageID    sql.NullInt64
		externalMessageID  sql.NullString
		sourceSegmentIndex sql.NullInt64
		polygonWKT         sql.NullString
		fipsCodes          models.StringSlice
		nwsZones           models.StringSlice
	)

	err := s.db.QueryRowContext(ctx, query, messageID, accountID).Scan(
		&item.ID,
		&item.Source,
		&item.EventCode,
		&item.MessageType,
		&item.AlertTypeCode,
		&title,
		&item.Status,
		&issuedAt,
		&item.ReceivedAt,
		&processedAt,
		&sourceMessageID,
		&externalMessageID,
		&sourceSegmentIndex,
		&polygonWKT,
		&fipsCodes,
		&nwsZones,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	item.Title = sqlutil.StringPtr(title)
	item.IssuedAt = sqlutil.TimePtr(issuedAt)
	item.ProcessedAt = sqlutil.TimePtr(processedAt)
	item.SourceMessageID = sqlutil.Int64Ptr(sourceMessageID)
	item.ExternalMessageID = sqlutil.StringPtr(externalMessageID)
	item.SourceSegmentIndex = sqlutil.IntPtr[int](sourceSegmentIndex)
	item.PolygonWKT = sqlutil.StringPtr(polygonWKT)
	item.FIPSCodes = []string(fipsCodes)
	item.NWSZones = []string(nwsZones)

	stats, err := s.getMessageCounts(ctx, accountID, messageID)
	if err != nil {
		return nil, err
	}
	item.Counts = stats
	return &item, nil
}

func (s *Server) getMessageCounts(ctx context.Context, accountID int64, messageID int64) (messageCounts, error) {
	counts := messageCounts{}

	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) AS recipients_count,
			SUM(CASE WHEN um.status = 'sent' THEN 1 ELSE 0 END) AS sent_recipients_count,
			SUM(CASE WHEN um.status = 'failed' THEN 1 ELSE 0 END) AS failed_recipients_count,
			SUM(CASE WHEN um.status = 'partial_failure' THEN 1 ELSE 0 END) AS partial_failure_recipients_count
		FROM users_messages um
		INNER JOIN users u
			ON u.id = um.user_id
		WHERE um.message_id = ?
		  AND u.account_id = ?`,
		messageID,
		accountID,
	).Scan(
		&counts.RecipientsCount,
		&counts.SentRecipientsCount,
		&counts.FailedRecipientsCount,
		&counts.PartialFailureRecipientsCount,
	)
	if err != nil {
		return counts, err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(da.id) AS attempts_count,
			COALESCE(SUM(CASE WHEN da.channel = 'sms' THEN 1 ELSE 0 END), 0) AS sms_attempts_count,
			COALESCE(SUM(CASE WHEN da.channel = 'email' THEN 1 ELSE 0 END), 0) AS email_attempts_count,
			COALESCE(SUM(CASE WHEN da.channel = 'voice' THEN 1 ELSE 0 END), 0) AS voice_attempts_count,
			COALESCE(SUM(CASE WHEN da.channel = 'sms' AND da.status = 'sent' THEN 1 ELSE 0 END), 0) AS sms_sent_count,
			COALESCE(SUM(CASE WHEN da.channel = 'email' AND da.status = 'sent' THEN 1 ELSE 0 END), 0) AS email_sent_count,
			COALESCE(SUM(CASE WHEN da.channel = 'voice' AND da.status = 'sent' THEN 1 ELSE 0 END), 0) AS voice_sent_count
		FROM users_messages um
		INNER JOIN users u
			ON u.id = um.user_id
		LEFT JOIN delivery_attempts da
			ON da.users_message_id = um.id
		WHERE um.message_id = ?
		  AND u.account_id = ?`,
		messageID,
		accountID,
	).Scan(
		&counts.AttemptsCount,
		&counts.SMSAttemptsCount,
		&counts.EmailAttemptsCount,
		&counts.VoiceAttemptsCount,
		&counts.SMSSentCount,
		&counts.EmailSentCount,
		&counts.VoiceSentCount,
	)
	return counts, err
}

func (s *Server) dashboardSummary(ctx context.Context, accountID int64, filter messageListFilter) (dashboardSummary, error) {
	whereSQL, whereArgs := buildMessageWhereClause(accountID, filter)

	query := `
		SELECT
			COUNT(*) AS messages_count,
			COALESCE(SUM(ums.recipients_count), 0) AS recipients_count,
			COALESCE(SUM(ums.sent_recipients_count), 0) AS sent_recipients_count,
			COALESCE(SUM(ums.failed_recipients_count), 0) AS failed_recipients_count,
			COALESCE(SUM(ums.partial_failure_recipients_count), 0) AS partial_failure_recipients_count,
			COALESCE(SUM(das.attempts_count), 0) AS attempts_count,
			COALESCE(SUM(das.sms_attempts_count), 0) AS sms_attempts_count,
			COALESCE(SUM(das.email_attempts_count), 0) AS email_attempts_count,
			COALESCE(SUM(das.voice_attempts_count), 0) AS voice_attempts_count,
			COALESCE(SUM(das.sms_sent_count), 0) AS sms_sent_count,
			COALESCE(SUM(das.email_sent_count), 0) AS email_sent_count,
			COALESCE(SUM(das.voice_sent_count), 0) AS voice_sent_count
		FROM messages m
		LEFT JOIN (
			SELECT
				um.message_id,
				COUNT(*) AS recipients_count,
				SUM(CASE WHEN um.status = 'sent' THEN 1 ELSE 0 END) AS sent_recipients_count,
				SUM(CASE WHEN um.status = 'failed' THEN 1 ELSE 0 END) AS failed_recipients_count,
				SUM(CASE WHEN um.status = 'partial_failure' THEN 1 ELSE 0 END) AS partial_failure_recipients_count
			FROM users_messages um
			INNER JOIN users u
				ON u.id = um.user_id
			WHERE u.account_id = ?
			GROUP BY um.message_id
		) ums
			ON ums.message_id = m.id
		LEFT JOIN (
			SELECT
				um.message_id,
				COUNT(da.id) AS attempts_count,
				SUM(CASE WHEN da.channel = 'sms' THEN 1 ELSE 0 END) AS sms_attempts_count,
				SUM(CASE WHEN da.channel = 'email' THEN 1 ELSE 0 END) AS email_attempts_count,
				SUM(CASE WHEN da.channel = 'voice' THEN 1 ELSE 0 END) AS voice_attempts_count,
				SUM(CASE WHEN da.channel = 'sms' AND da.status = 'sent' THEN 1 ELSE 0 END) AS sms_sent_count,
				SUM(CASE WHEN da.channel = 'email' AND da.status = 'sent' THEN 1 ELSE 0 END) AS email_sent_count,
				SUM(CASE WHEN da.channel = 'voice' AND da.status = 'sent' THEN 1 ELSE 0 END) AS voice_sent_count
			FROM users_messages um
			INNER JOIN users u
				ON u.id = um.user_id
			LEFT JOIN delivery_attempts da
				ON da.users_message_id = um.id
			WHERE u.account_id = ?
			GROUP BY um.message_id
		) das
			ON das.message_id = m.id
		WHERE ` + whereSQL

	args := make([]any, 0, 2+len(whereArgs))
	args = append(args, accountID, accountID)
	args = append(args, whereArgs...)

	var summary dashboardSummary
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.MessagesCount,
		&summary.RecipientsCount,
		&summary.SentRecipientsCount,
		&summary.FailedRecipientsCount,
		&summary.PartialFailureRecipientsCount,
		&summary.AttemptsCount,
		&summary.SMSAttemptsCount,
		&summary.EmailAttemptsCount,
		&summary.VoiceAttemptsCount,
		&summary.SMSSentCount,
		&summary.EmailSentCount,
		&summary.VoiceSentCount,
	)
	return summary, err
}

func (s *Server) listLocations(ctx context.Context, accountID int64, filter locationListFilter) ([]locationListItem, int64, error) {
	whereSQL, whereArgs := buildLocationWhereClause(accountID, filter)

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations l WHERE `+whereSQL, whereArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT
			l.id,
			l.name,
			l.address_line_1,
			l.address_line_2,
			l.city,
			l.state_code,
			l.postal_code,
			l.county_fips,
			l.nws_zone,
			l.latitude,
			l.longitude,
			ST_AsText(l.coverage_geometry) AS coverage_wkt,
			l.is_thundercall_enabled,
			l.active,
			COUNT(DISTINCT CASE WHEN u.id IS NOT NULL THEN ul.user_id END) AS subscribed_users_count
		FROM locations l
		LEFT JOIN users_locations ul
			ON ul.location_id = l.id
		   AND ul.is_thundercall_enabled = 1
		LEFT JOIN users u
			ON u.id = ul.user_id
		   AND u.account_id = l.account_id
		   AND u.active = 1
		WHERE ` + whereSQL + `
		GROUP BY
			l.id, l.name, l.address_line_1, l.address_line_2, l.city, l.state_code, l.postal_code,
			l.county_fips, l.nws_zone, l.latitude, l.longitude, coverage_wkt, l.is_thundercall_enabled, l.active
		ORDER BY l.name, l.id
		LIMIT ? OFFSET ?`

	args := append(append([]any(nil), whereArgs...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]locationListItem, 0, filter.Limit)
	for rows.Next() {
		item, err := scanLocationListItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}

func (s *Server) getLocationDetail(ctx context.Context, accountID int64, locationID int64) (*locationListItem, error) {
	query := `
		SELECT
			l.id,
			l.name,
			l.address_line_1,
			l.address_line_2,
			l.city,
			l.state_code,
			l.postal_code,
			l.county_fips,
			l.nws_zone,
			l.latitude,
			l.longitude,
			ST_AsText(l.coverage_geometry) AS coverage_wkt,
			l.is_thundercall_enabled,
			l.active,
			COUNT(DISTINCT CASE WHEN u.id IS NOT NULL THEN ul.user_id END) AS subscribed_users_count
		FROM locations l
		LEFT JOIN users_locations ul
			ON ul.location_id = l.id
		   AND ul.is_thundercall_enabled = 1
		LEFT JOIN users u
			ON u.id = ul.user_id
		   AND u.account_id = l.account_id
		   AND u.active = 1
		WHERE l.account_id = ?
		  AND l.id = ?
		GROUP BY
			l.id, l.name, l.address_line_1, l.address_line_2, l.city, l.state_code, l.postal_code,
			l.county_fips, l.nws_zone, l.latitude, l.longitude, coverage_wkt, l.is_thundercall_enabled, l.active`

	row := s.db.QueryRowContext(ctx, query, accountID, locationID)
	item, err := scanLocationListItem(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (s *Server) listMessageLocations(ctx context.Context, accountID int64, messageID int64) ([]messageLocationItem, error) {
	query := `
		SELECT
			l.id,
			l.name,
			l.address_line_1,
			l.address_line_2,
			l.city,
			l.state_code,
			l.postal_code,
			l.county_fips,
			l.nws_zone,
			l.latitude,
			l.longitude,
			ST_AsText(l.coverage_geometry) AS coverage_wkt,
			l.is_thundercall_enabled,
			l.active,
			COUNT(DISTINCT um.user_id) AS matched_users_count,
			0 AS sms_enabled_count,
			0 AS email_enabled_count,
			SUM(CASE WHEN um.voice_enabled = 1 THEN 1 ELSE 0 END) AS voice_enabled_count
		FROM users_messages um
		INNER JOIN users u
			ON u.id = um.user_id
		INNER JOIN locations l
			ON l.id = um.matched_location_id
		WHERE um.message_id = ?
		  AND u.account_id = ?
		GROUP BY
			l.id, l.name, l.address_line_1, l.address_line_2, l.city, l.state_code, l.postal_code,
			l.county_fips, l.nws_zone, l.latitude, l.longitude, coverage_wkt, l.is_thundercall_enabled, l.active
		ORDER BY matched_users_count DESC, l.name, l.id`

	rows, err := s.db.QueryContext(ctx, query, messageID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []messageLocationItem
	for rows.Next() {
		item, err := scanMessageLocationItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) listMessageDeliveries(ctx context.Context, accountID int64, messageID int64, filter deliveryListFilter) ([]messageDeliveryItem, int64, error) {
	whereSQL, whereArgs := buildDeliveryWhereClause(accountID, messageID, filter)

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users_messages um INNER JOIN users u ON u.id = um.user_id LEFT JOIN locations l ON l.id = um.matched_location_id WHERE `+whereSQL, whereArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT
			um.id,
			um.user_id,
			u.display_name,
			u.first_name,
			u.last_name,
			u.title,
			um.status,
			um.queued_at,
			um.delivered_at,
			FALSE AS sms_enabled,
			FALSE AS email_enabled,
			um.voice_enabled,
			l.id,
			l.name
		FROM users_messages um
		INNER JOIN users u
			ON u.id = um.user_id
		LEFT JOIN locations l
			ON l.id = um.matched_location_id
		WHERE ` + whereSQL + `
		ORDER BY um.queued_at DESC, um.id DESC
		LIMIT ? OFFSET ?`

	args := append(append([]any(nil), whereArgs...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]messageDeliveryItem, 0, filter.Limit)
	userMessageIDs := make([]int64, 0, filter.Limit)
	for rows.Next() {
		item, err := scanMessageDeliveryItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
		userMessageIDs = append(userMessageIDs, item.UserMessageID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	attemptsByUserMessage, err := s.listDeliveryAttemptsByUserMessageIDs(ctx, userMessageIDs)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].Attempts = attemptsByUserMessage[items[i].UserMessageID]
	}

	return items, total, nil
}

func (s *Server) listDeliveryAttemptsByUserMessageIDs(ctx context.Context, userMessageIDs []int64) (map[int64][]deliveryAttemptItem, error) {
	if len(userMessageIDs) == 0 {
		return map[int64][]deliveryAttemptItem{}, nil
	}

	args := make([]any, 0, len(userMessageIDs))
	for _, id := range userMessageIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT
			id,
			users_message_id,
			channel,
			destination,
			provider,
			provider_message_id,
			status,
			error_message,
			requested_at,
			sent_at,
			delivered_at
		FROM delivery_attempts
		WHERE users_message_id IN (%s)
		ORDER BY users_message_id, id`, sqlutil.Placeholders(len(userMessageIDs)))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]deliveryAttemptItem, len(userMessageIDs))
	for rows.Next() {
		var (
			item              deliveryAttemptItem
			userMessageID     int64
			provider          sql.NullString
			providerMessageID sql.NullString
			errorMessage      sql.NullString
			sentAt            sql.NullTime
			deliveredAt       sql.NullTime
		)

		if err := rows.Scan(
			&item.ID,
			&userMessageID,
			&item.Channel,
			&item.Destination,
			&provider,
			&providerMessageID,
			&item.Status,
			&errorMessage,
			&item.RequestedAt,
			&sentAt,
			&deliveredAt,
		); err != nil {
			return nil, err
		}

		item.Provider = sqlutil.StringPtr(provider)
		item.ProviderMessageID = sqlutil.StringPtr(providerMessageID)
		item.ErrorMessage = sqlutil.StringPtr(errorMessage)
		item.SentAt = sqlutil.TimePtr(sentAt)
		item.DeliveredAt = sqlutil.TimePtr(deliveredAt)
		result[userMessageID] = append(result[userMessageID], item)
	}

	return result, rows.Err()
}

func (s *Server) messageVisibleToAccount(ctx context.Context, accountID int64, messageID int64) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT m.id
		FROM messages m
		WHERE m.id = ?
		  AND EXISTS (
			SELECT 1
			FROM users_messages um
			INNER JOIN users u
				ON u.id = um.user_id
			WHERE um.message_id = m.id
			  AND u.account_id = ?
		  )`,
		messageID,
		accountID,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func buildMessageWhereClause(accountID int64, filter messageListFilter) (string, []any) {
	clauses := []string{
		`EXISTS (
			SELECT 1
			FROM users_messages um
			INNER JOIN users u
				ON u.id = um.user_id
			WHERE um.message_id = m.id
			  AND u.account_id = ?
		)`,
	}
	args := []any{accountID}

	if filter.From != nil {
		clauses = append(clauses, `COALESCE(m.issued_at, m.received_at) >= ?`)
		args = append(args, filter.From.UTC())
	}
	if filter.To != nil {
		clauses = append(clauses, `COALESCE(m.issued_at, m.received_at) <= ?`)
		args = append(args, filter.To.UTC())
	}
	if value := strings.TrimSpace(filter.EventCode); value != "" {
		clauses = append(clauses, `m.event_code = ?`)
		args = append(args, strings.ToUpper(value))
	}
	if value := strings.TrimSpace(filter.MessageType); value != "" {
		clauses = append(clauses, `m.message_type = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, `m.status = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Source); value != "" {
		clauses = append(clauses, `m.source = ?`)
		args = append(args, strings.ToUpper(value))
	}
	if search := strings.ToLower(strings.TrimSpace(filter.Search)); search != "" {
		like := "%" + search + "%"
		clauses = append(clauses, `(LOWER(m.event_code) LIKE ? OR LOWER(m.message_type) LIKE ? OR LOWER(COALESCE(m.title, '')) LIKE ? OR LOWER(m.body) LIKE ?)`)
		args = append(args, like, like, like, like)
	}

	return strings.Join(clauses, " AND "), args
}

func buildLocationWhereClause(accountID int64, filter locationListFilter) (string, []any) {
	clauses := []string{`l.account_id = ?`}
	args := []any{accountID}

	if filter.ActiveOnly != nil {
		clauses = append(clauses, `l.active = ?`)
		args = append(args, *filter.ActiveOnly)
	}
	if search := strings.ToLower(strings.TrimSpace(filter.Search)); search != "" {
		like := "%" + search + "%"
		clauses = append(clauses, `(LOWER(l.name) LIKE ? OR LOWER(COALESCE(l.city, '')) LIKE ? OR LOWER(COALESCE(l.state_code, '')) LIKE ? OR LOWER(COALESCE(l.county_fips, '')) LIKE ? OR LOWER(COALESCE(l.nws_zone, '')) LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}

	return strings.Join(clauses, " AND "), args
}

func buildDeliveryWhereClause(accountID int64, messageID int64, filter deliveryListFilter) (string, []any) {
	clauses := []string{
		`um.message_id = ?`,
		`u.account_id = ?`,
	}
	args := []any{messageID, accountID}

	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, `um.status = ?`)
		args = append(args, value)
	}
	if search := strings.ToLower(strings.TrimSpace(filter.Search)); search != "" {
		like := "%" + search + "%"
		clauses = append(clauses, `(LOWER(COALESCE(u.display_name, '')) LIKE ? OR LOWER(COALESCE(u.first_name, '')) LIKE ? OR LOWER(COALESCE(u.last_name, '')) LIKE ? OR LOWER(COALESCE(l.name, '')) LIKE ?)`)
		args = append(args, like, like, like, like)
	}

	return strings.Join(clauses, " AND "), args
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMessageListItem(s scanner) (messageListItem, error) {
	var (
		item               messageListItem
		title              sql.NullString
		issuedAt           sql.NullTime
		processedAt        sql.NullTime
		sourceMessageID    sql.NullInt64
		externalMessageID  sql.NullString
		sourceSegmentIndex sql.NullInt64
		polygonWKT         sql.NullString
		fipsCodes          models.StringSlice
		nwsZones           models.StringSlice
	)

	err := s.Scan(
		&item.ID,
		&item.Source,
		&item.EventCode,
		&item.MessageType,
		&item.AlertTypeCode,
		&title,
		&item.Status,
		&issuedAt,
		&item.ReceivedAt,
		&processedAt,
		&sourceMessageID,
		&externalMessageID,
		&sourceSegmentIndex,
		&polygonWKT,
		&fipsCodes,
		&nwsZones,
		&item.Counts.RecipientsCount,
		&item.Counts.SentRecipientsCount,
		&item.Counts.FailedRecipientsCount,
		&item.Counts.PartialFailureRecipientsCount,
		&item.Counts.AttemptsCount,
		&item.Counts.SMSAttemptsCount,
		&item.Counts.EmailAttemptsCount,
		&item.Counts.VoiceAttemptsCount,
		&item.Counts.SMSSentCount,
		&item.Counts.EmailSentCount,
		&item.Counts.VoiceSentCount,
	)
	if err != nil {
		return messageListItem{}, err
	}

	item.Title = sqlutil.StringPtr(title)
	item.IssuedAt = sqlutil.TimePtr(issuedAt)
	item.ProcessedAt = sqlutil.TimePtr(processedAt)
	item.SourceMessageID = sqlutil.Int64Ptr(sourceMessageID)
	item.ExternalMessageID = sqlutil.StringPtr(externalMessageID)
	item.SourceSegmentIndex = sqlutil.IntPtr[int](sourceSegmentIndex)
	item.PolygonWKT = sqlutil.StringPtr(polygonWKT)
	item.FIPSCodes = []string(fipsCodes)
	item.NWSZones = []string(nwsZones)
	return item, nil
}

func scanLocationListItem(s scanner) (locationListItem, error) {
	var (
		item         locationListItem
		addressLine1 sql.NullString
		addressLine2 sql.NullString
		city         sql.NullString
		stateCode    sql.NullString
		postalCode   sql.NullString
		countyFIPS   sql.NullString
		nwsZone      sql.NullString
		latitude     sql.NullFloat64
		longitude    sql.NullFloat64
		coverageWKT  sql.NullString
	)

	err := s.Scan(
		&item.ID,
		&item.Name,
		&addressLine1,
		&addressLine2,
		&city,
		&stateCode,
		&postalCode,
		&countyFIPS,
		&nwsZone,
		&latitude,
		&longitude,
		&coverageWKT,
		&item.IsThunderCallEnabled,
		&item.Active,
		&item.SubscribedUsersCount,
	)
	if err != nil {
		return locationListItem{}, err
	}

	item.AddressLine1 = sqlutil.StringPtr(addressLine1)
	item.AddressLine2 = sqlutil.StringPtr(addressLine2)
	item.City = sqlutil.StringPtr(city)
	item.StateCode = sqlutil.StringPtr(stateCode)
	item.PostalCode = sqlutil.StringPtr(postalCode)
	item.CountyFIPS = sqlutil.StringPtr(countyFIPS)
	item.NWSZone = sqlutil.StringPtr(nwsZone)
	item.Latitude = sqlutil.Float64Ptr(latitude)
	item.Longitude = sqlutil.Float64Ptr(longitude)
	item.CoverageWKT = sqlutil.StringPtr(coverageWKT)
	return item, nil
}

func scanMessageLocationItem(s scanner) (messageLocationItem, error) {
	var (
		item         messageLocationItem
		addressLine1 sql.NullString
		addressLine2 sql.NullString
		city         sql.NullString
		stateCode    sql.NullString
		postalCode   sql.NullString
		countyFIPS   sql.NullString
		nwsZone      sql.NullString
		latitude     sql.NullFloat64
		longitude    sql.NullFloat64
		coverageWKT  sql.NullString
	)

	err := s.Scan(
		&item.ID,
		&item.Name,
		&addressLine1,
		&addressLine2,
		&city,
		&stateCode,
		&postalCode,
		&countyFIPS,
		&nwsZone,
		&latitude,
		&longitude,
		&coverageWKT,
		&item.IsThunderCallEnabled,
		&item.Active,
		&item.MatchedUsersCount,
		&item.SMSEnabledCount,
		&item.EmailEnabledCount,
		&item.VoiceEnabledCount,
	)
	if err != nil {
		return messageLocationItem{}, err
	}

	item.AddressLine1 = sqlutil.StringPtr(addressLine1)
	item.AddressLine2 = sqlutil.StringPtr(addressLine2)
	item.City = sqlutil.StringPtr(city)
	item.StateCode = sqlutil.StringPtr(stateCode)
	item.PostalCode = sqlutil.StringPtr(postalCode)
	item.CountyFIPS = sqlutil.StringPtr(countyFIPS)
	item.NWSZone = sqlutil.StringPtr(nwsZone)
	item.Latitude = sqlutil.Float64Ptr(latitude)
	item.Longitude = sqlutil.Float64Ptr(longitude)
	item.CoverageWKT = sqlutil.StringPtr(coverageWKT)
	return item, nil
}

func scanMessageDeliveryItem(s scanner) (messageDeliveryItem, error) {
	var (
		item         messageDeliveryItem
		displayName  sql.NullString
		firstName    sql.NullString
		lastName     sql.NullString
		title        sql.NullString
		deliveredAt  sql.NullTime
		locationID   sql.NullInt64
		locationName sql.NullString
	)

	err := s.Scan(
		&item.UserMessageID,
		&item.UserID,
		&displayName,
		&firstName,
		&lastName,
		&title,
		&item.Status,
		&item.QueuedAt,
		&deliveredAt,
		&item.SMSEnabled,
		&item.EmailEnabled,
		&item.VoiceEnabled,
		&locationID,
		&locationName,
	)
	if err != nil {
		return messageDeliveryItem{}, err
	}

	item.DisplayName = preferredDisplayName(sqlutil.StringPtr(displayName), sqlutil.StringPtr(firstName), sqlutil.StringPtr(lastName), item.UserID)
	item.Title = sqlutil.StringPtr(title)
	item.DeliveredAt = sqlutil.TimePtr(deliveredAt)
	if locationID.Valid {
		item.MatchedLocation = &matchedLocationInfo{
			ID:   locationID.Int64,
			Name: locationName.String,
		}
	}
	return item, nil
}

func preferredDisplayName(displayName *string, firstName *string, lastName *string, userID int64) string {
	if displayName != nil && strings.TrimSpace(*displayName) != "" {
		return strings.TrimSpace(*displayName)
	}

	fullName := strings.TrimSpace(strings.Join([]string{stringValue(firstName), stringValue(lastName)}, " "))
	if fullName != "" {
		return fullName
	}

	return fmt.Sprintf("User %d", userID)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

package deliveryattempts

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"thundercall-go/internal/models"
	"thundercall-go/internal/repositories/sqlutil"
)

type Repository struct {
	db sqlutil.DBTX
}

type VoiceDispatchRecord struct {
	Attempt        models.DeliveryAttempt
	MessageID      int64
	AccountID      *int64
	UserID         int64
	EventCode      string
	AlertTypeCode  string
	MessageBody    string
	MessageType    string
	MessageTitle   *string
	ReceivedAt     time.Time
	NWSEventID     *int64
	NotificationID *int64
}

type VoiceCallbackUpdate struct {
	Status                  string
	ProviderStatus          *string
	ProviderAnsweredBy      *string
	ProviderDurationSeconds *int
	ErrorMessage            *string
	ProviderPayloadJSON     *string
	ProviderLastCallbackAt  time.Time
	SentAt                  *time.Time
	DeliveredAt             *time.Time
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func NewWithDBTX(db sqlutil.DBTX) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, attempt *models.DeliveryAttempt) error {
	if attempt.AttemptNumber <= 0 {
		attempt.AttemptNumber = 1
	}
	if attempt.DispatchAfter.IsZero() {
		attempt.DispatchAfter = attempt.RequestedAt
	}
	if attempt.DispatchAfter.IsZero() {
		attempt.DispatchAfter = time.Now().UTC()
	}

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO delivery_attempts (
			users_message_id, notification_id, channel, attempt_number, destination, provider, provider_message_id,
			status, provider_status, provider_answered_by, provider_duration_seconds, error_message, provider_payload_json, provider_last_callback_at,
			requested_at, dispatch_after, lease_token, lease_owner, lease_expires_at, sent_at, delivered_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.UserMessageID,
		sqlutil.Int64Value(attempt.NotificationID),
		string(attempt.Channel),
		attempt.AttemptNumber,
		attempt.Destination,
		sqlutil.StringValue(attempt.Provider),
		sqlutil.StringValue(attempt.ProviderMessageID),
		attempt.Status,
		sqlutil.StringValue(attempt.ProviderStatus),
		sqlutil.StringValue(attempt.ProviderAnsweredBy),
		sqlutil.IntValue(attempt.ProviderDurationSeconds),
		sqlutil.StringValue(attempt.ErrorMessage),
		sqlutil.StringValue(attempt.ProviderPayloadJSON),
		sqlutil.TimeValue(attempt.ProviderLastCallbackAt),
		attempt.RequestedAt,
		attempt.DispatchAfter,
		sqlutil.StringValue(attempt.LeaseToken),
		sqlutil.StringValue(attempt.LeaseOwner),
		sqlutil.TimeValue(attempt.LeaseExpiresAt),
		sqlutil.TimeValue(attempt.SentAt),
		sqlutil.TimeValue(attempt.DeliveredAt),
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	attempt.ID = id
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id int64) (*models.DeliveryAttempt, error) {
	row := r.db.QueryRowContext(ctx, selectDeliveryAttemptSQL()+` WHERE id = ?`, id)
	return scanDeliveryAttempt(row)
}

func (r *Repository) GetByUserMessageIDAndChannel(ctx context.Context, userMessageID int64, channel models.Channel) (*models.DeliveryAttempt, error) {
	row := r.db.QueryRowContext(ctx, selectDeliveryAttemptSQL()+` WHERE users_message_id = ? AND channel = ? ORDER BY attempt_number DESC, id DESC LIMIT 1`, userMessageID, string(channel))
	return scanDeliveryAttempt(row)
}

func (r *Repository) UpdateStatus(ctx context.Context, id int64, status string, providerMessageID *string, errorMessage *string, sentAt *time.Time, deliveredAt *time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE delivery_attempts
		 SET status = ?,
		     provider_message_id = ?,
		     error_message = ?,
		     lease_token = NULL,
		     lease_owner = NULL,
		     lease_expires_at = NULL,
		     sent_at = ?,
		     delivered_at = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		status,
		sqlutil.StringValue(providerMessageID),
		sqlutil.StringValue(errorMessage),
		sqlutil.TimeValue(sentAt),
		sqlutil.TimeValue(deliveredAt),
		id,
	)
	return err
}

func (r *Repository) UpdateVoiceCallback(ctx context.Context, id int64, update VoiceCallbackUpdate) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE delivery_attempts
		 SET status = ?,
		     provider_status = ?,
		     provider_answered_by = ?,
		     provider_duration_seconds = ?,
		     error_message = ?,
		     provider_payload_json = ?,
		     provider_last_callback_at = ?,
		     sent_at = ?,
		     delivered_at = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		update.Status,
		sqlutil.StringValue(update.ProviderStatus),
		sqlutil.StringValue(update.ProviderAnsweredBy),
		sqlutil.IntValue(update.ProviderDurationSeconds),
		sqlutil.StringValue(update.ErrorMessage),
		sqlutil.StringValue(update.ProviderPayloadJSON),
		update.ProviderLastCallbackAt,
		sqlutil.TimeValue(update.SentAt),
		sqlutil.TimeValue(update.DeliveredAt),
		id,
	)
	return err
}

func (r *Repository) UpdateDestination(ctx context.Context, id int64, destination string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE delivery_attempts
		 SET destination = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		destination,
		id,
	)
	return err
}

func (r *Repository) Requeue(ctx context.Context, id int64, errorMessage *string, dispatchAfter time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE delivery_attempts
		 SET status = 'queued',
		     error_message = ?,
		     dispatch_after = ?,
		     lease_token = NULL,
		     lease_owner = NULL,
		     lease_expires_at = NULL,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		sqlutil.StringValue(errorMessage),
		dispatchAfter,
		id,
	)
	return err
}

func (r *Repository) ListByUserMessageID(ctx context.Context, userMessageID int64) ([]models.DeliveryAttempt, error) {
	rows, err := r.db.QueryContext(ctx, selectDeliveryAttemptSQL()+`
	 WHERE users_message_id = ?
	 ORDER BY attempt_number, id`, userMessageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []models.DeliveryAttempt
	for rows.Next() {
		attempt, err := scanDeliveryAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, *attempt)
	}

	return attempts, rows.Err()
}

func (r *Repository) ListByNotificationID(ctx context.Context, notificationID int64) ([]models.DeliveryAttempt, error) {
	rows, err := r.db.QueryContext(ctx, selectDeliveryAttemptSQL()+`
	 WHERE notification_id = ?
	 ORDER BY attempt_number, id`, notificationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []models.DeliveryAttempt
	for rows.Next() {
		attempt, err := scanDeliveryAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, *attempt)
	}

	return attempts, rows.Err()
}

func (r *Repository) ListByProviderMessageID(ctx context.Context, providerMessageID string) ([]models.DeliveryAttempt, error) {
	rows, err := r.db.QueryContext(ctx, selectDeliveryAttemptSQL()+`
	 WHERE provider_message_id = ?
	 ORDER BY id`, providerMessageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []models.DeliveryAttempt
	for rows.Next() {
		attempt, err := scanDeliveryAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, *attempt)
	}

	return attempts, rows.Err()
}

func (r *Repository) ListVoiceDispatchRecordsByProviderMessageID(ctx context.Context, providerMessageID string) ([]VoiceDispatchRecord, error) {
	rows, err := r.db.QueryContext(
		ctx,
		selectVoiceDispatchSQL()+`
		WHERE da.provider_message_id = ?
		ORDER BY da.id`,
		providerMessageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []VoiceDispatchRecord
	for rows.Next() {
		record, err := scanVoiceDispatchRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}

	return records, rows.Err()
}

func (r *Repository) ClaimQueuedVoiceAttempts(ctx context.Context, leaseToken string, leaseOwner string, now time.Time, leaseDuration time.Duration, limit int) ([]VoiceDispatchRecord, error) {
	if limit <= 0 {
		return nil, nil
	}

	leaseExpiresAt := now.Add(leaseDuration)
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE delivery_attempts da
		JOIN (
			SELECT ranked.id
			FROM (
				SELECT
					da.id,
					ROW_NUMBER() OVER (
						PARTITION BY um.message_id
						ORDER BY da.requested_at, da.id
					) AS queue_rank,
					m.received_at
				FROM delivery_attempts da
				INNER JOIN users_messages um
					ON um.id = da.users_message_id
				INNER JOIN messages m
					ON m.id = um.message_id
				WHERE da.channel = 'voice'
				  AND da.status = 'queued'
				  AND da.dispatch_after <= ?
				  AND (da.lease_expires_at IS NULL OR da.lease_expires_at <= ?)
			) ranked
			ORDER BY ranked.queue_rank, ranked.received_at, ranked.id
			LIMIT ?
		) picked
			ON picked.id = da.id
		SET da.status = 'dispatching',
		    da.lease_token = ?,
		    da.lease_owner = ?,
		    da.lease_expires_at = ?,
		    da.updated_at = CURRENT_TIMESTAMP
		WHERE da.channel = 'voice'
		  AND da.status = 'queued'
		  AND da.dispatch_after <= ?
		  AND (da.lease_expires_at IS NULL OR da.lease_expires_at <= ?)`,
		now,
		now,
		limit,
		leaseToken,
		leaseOwner,
		leaseExpiresAt,
		now,
		now,
	)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(
		ctx,
		selectVoiceDispatchSQL()+`
		WHERE da.lease_token = ?
		ORDER BY queue_rank, m.received_at, da.id`,
		leaseToken,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []VoiceDispatchRecord
	for rows.Next() {
		record, err := scanVoiceDispatchRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}

	return records, rows.Err()
}

func (r *Repository) GetLatestSentVoiceAttemptByMessageID(ctx context.Context, messageID int64) (*VoiceDispatchRecord, error) {
	row := r.db.QueryRowContext(
		ctx,
		selectVoiceDispatchSQL()+`
		WHERE um.message_id = ?
		  AND da.channel = 'voice'
		  AND da.status = 'sent'
		ORDER BY da.sent_at DESC, da.id DESC
		LIMIT 1`,
		messageID,
	)
	return scanVoiceDispatchRecord(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func selectDeliveryAttemptSQL() string {
	return `
		SELECT id, users_message_id, notification_id, channel, attempt_number, destination, provider, provider_message_id,
		       status, provider_status, provider_answered_by, provider_duration_seconds, error_message, provider_payload_json, provider_last_callback_at,
		       requested_at, dispatch_after, lease_token, lease_owner, lease_expires_at,
		       sent_at, delivered_at, created_at, updated_at
		FROM delivery_attempts`
}

func selectVoiceDispatchSQL() string {
	return `
		SELECT
			da.id,
			da.users_message_id,
			da.notification_id,
			da.channel,
			da.attempt_number,
			da.destination,
			da.provider,
			da.provider_message_id,
			da.status,
			da.provider_status,
			da.provider_answered_by,
			da.provider_duration_seconds,
			da.error_message,
			da.provider_payload_json,
			da.provider_last_callback_at,
			da.requested_at,
			da.dispatch_after,
			da.lease_token,
			da.lease_owner,
			da.lease_expires_at,
			da.sent_at,
			da.delivered_at,
			da.created_at,
			da.updated_at,
			um.message_id,
			um.user_id,
			m.account_id,
			m.nws_event_id,
			m.event_code,
			m.alert_type_code,
			m.message_type,
			m.title,
			m.body,
			m.received_at,
			ROW_NUMBER() OVER (
				PARTITION BY um.message_id
				ORDER BY da.requested_at, da.id
			) AS queue_rank
		FROM delivery_attempts da
		INNER JOIN users_messages um
			ON um.id = da.users_message_id
		INNER JOIN messages m
			ON m.id = um.message_id`
}

func scanDeliveryAttempt(s scanner) (*models.DeliveryAttempt, error) {
	var (
		attempt                 models.DeliveryAttempt
		notificationID          sql.NullInt64
		channel                 string
		provider                sql.NullString
		providerMessageID       sql.NullString
		providerStatus          sql.NullString
		providerAnsweredBy      sql.NullString
		providerDurationSeconds sql.NullInt64
		errorMessage            sql.NullString
		providerPayloadJSON     sql.NullString
		providerLastCallbackAt  sql.NullTime
		leaseToken              sql.NullString
		leaseOwner              sql.NullString
		sentAt                  sql.NullTime
		dispatchAfter           sql.NullTime
		leaseExpiresAt          sql.NullTime
		deliveredAt             sql.NullTime
	)

	if err := s.Scan(
		&attempt.ID,
		&attempt.UserMessageID,
		&notificationID,
		&channel,
		&attempt.AttemptNumber,
		&attempt.Destination,
		&provider,
		&providerMessageID,
		&attempt.Status,
		&providerStatus,
		&providerAnsweredBy,
		&providerDurationSeconds,
		&errorMessage,
		&providerPayloadJSON,
		&providerLastCallbackAt,
		&attempt.RequestedAt,
		&dispatchAfter,
		&leaseToken,
		&leaseOwner,
		&leaseExpiresAt,
		&sentAt,
		&deliveredAt,
		&attempt.CreatedAt,
		&attempt.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	attempt.Channel = models.Channel(channel)
	attempt.NotificationID = sqlutil.Int64Ptr(notificationID)
	attempt.Provider = sqlutil.StringPtr(provider)
	attempt.ProviderMessageID = sqlutil.StringPtr(providerMessageID)
	attempt.ProviderStatus = sqlutil.StringPtr(providerStatus)
	attempt.ProviderAnsweredBy = sqlutil.StringPtr(providerAnsweredBy)
	attempt.ProviderDurationSeconds = sqlutil.IntPtr[int](providerDurationSeconds)
	attempt.ErrorMessage = sqlutil.StringPtr(errorMessage)
	attempt.ProviderPayloadJSON = sqlutil.StringPtr(providerPayloadJSON)
	attempt.ProviderLastCallbackAt = sqlutil.TimePtr(providerLastCallbackAt)
	attempt.DispatchAfter = timeOrZero(dispatchAfter)
	attempt.LeaseToken = sqlutil.StringPtr(leaseToken)
	attempt.LeaseOwner = sqlutil.StringPtr(leaseOwner)
	attempt.LeaseExpiresAt = sqlutil.TimePtr(leaseExpiresAt)
	attempt.SentAt = sqlutil.TimePtr(sentAt)
	attempt.DeliveredAt = sqlutil.TimePtr(deliveredAt)
	return &attempt, nil
}

func scanVoiceDispatchRecord(s scanner) (*VoiceDispatchRecord, error) {
	var (
		record                  VoiceDispatchRecord
		notificationID          sql.NullInt64
		channel                 string
		provider                sql.NullString
		providerMessageID       sql.NullString
		providerStatus          sql.NullString
		providerAnsweredBy      sql.NullString
		providerDurationSeconds sql.NullInt64
		errorMessage            sql.NullString
		providerPayloadJSON     sql.NullString
		providerLastCallbackAt  sql.NullTime
		dispatchAfter           sql.NullTime
		leaseToken              sql.NullString
		leaseOwner              sql.NullString
		leaseExpiresAt          sql.NullTime
		sentAt                  sql.NullTime
		deliveredAt             sql.NullTime
		accountID               sql.NullInt64
		nwsEventID              sql.NullInt64
		title                   sql.NullString
		queueRank               int
	)

	if err := s.Scan(
		&record.Attempt.ID,
		&record.Attempt.UserMessageID,
		&notificationID,
		&channel,
		&record.Attempt.AttemptNumber,
		&record.Attempt.Destination,
		&provider,
		&providerMessageID,
		&record.Attempt.Status,
		&providerStatus,
		&providerAnsweredBy,
		&providerDurationSeconds,
		&errorMessage,
		&providerPayloadJSON,
		&providerLastCallbackAt,
		&record.Attempt.RequestedAt,
		&dispatchAfter,
		&leaseToken,
		&leaseOwner,
		&leaseExpiresAt,
		&sentAt,
		&deliveredAt,
		&record.Attempt.CreatedAt,
		&record.Attempt.UpdatedAt,
		&record.MessageID,
		&record.UserID,
		&accountID,
		&nwsEventID,
		&record.EventCode,
		&record.AlertTypeCode,
		&record.MessageType,
		&title,
		&record.MessageBody,
		&record.ReceivedAt,
		&queueRank,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	record.Attempt.Channel = models.Channel(channel)
	record.Attempt.NotificationID = sqlutil.Int64Ptr(notificationID)
	record.Attempt.Provider = sqlutil.StringPtr(provider)
	record.Attempt.ProviderMessageID = sqlutil.StringPtr(providerMessageID)
	record.Attempt.ProviderStatus = sqlutil.StringPtr(providerStatus)
	record.Attempt.ProviderAnsweredBy = sqlutil.StringPtr(providerAnsweredBy)
	record.Attempt.ProviderDurationSeconds = sqlutil.IntPtr[int](providerDurationSeconds)
	record.Attempt.ErrorMessage = sqlutil.StringPtr(errorMessage)
	record.Attempt.ProviderPayloadJSON = sqlutil.StringPtr(providerPayloadJSON)
	record.Attempt.ProviderLastCallbackAt = sqlutil.TimePtr(providerLastCallbackAt)
	record.Attempt.DispatchAfter = timeOrZero(dispatchAfter)
	record.Attempt.LeaseToken = sqlutil.StringPtr(leaseToken)
	record.Attempt.LeaseOwner = sqlutil.StringPtr(leaseOwner)
	record.Attempt.LeaseExpiresAt = sqlutil.TimePtr(leaseExpiresAt)
	record.Attempt.SentAt = sqlutil.TimePtr(sentAt)
	record.Attempt.DeliveredAt = sqlutil.TimePtr(deliveredAt)
	record.AccountID = sqlutil.Int64Ptr(accountID)
	record.NWSEventID = sqlutil.Int64Ptr(nwsEventID)
	record.NotificationID = record.Attempt.NotificationID
	record.MessageTitle = sqlutil.StringPtr(title)
	return &record, nil
}

func timeOrZero(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

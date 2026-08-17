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

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO delivery_attempts (
			users_message_id, notification_id, channel, attempt_number, destination, provider, provider_message_id,
			status, error_message, requested_at, sent_at, delivered_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.UserMessageID,
		sqlutil.Int64Value(attempt.NotificationID),
		string(attempt.Channel),
		attempt.AttemptNumber,
		attempt.Destination,
		sqlutil.StringValue(attempt.Provider),
		sqlutil.StringValue(attempt.ProviderMessageID),
		attempt.Status,
		sqlutil.StringValue(attempt.ErrorMessage),
		attempt.RequestedAt,
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

type scanner interface {
	Scan(dest ...any) error
}

func selectDeliveryAttemptSQL() string {
	return `
		SELECT id, users_message_id, notification_id, channel, attempt_number, destination, provider, provider_message_id,
		       status, error_message, requested_at, sent_at, delivered_at, created_at, updated_at
		FROM delivery_attempts`
}

func scanDeliveryAttempt(s scanner) (*models.DeliveryAttempt, error) {
	var (
		attempt           models.DeliveryAttempt
		notificationID    sql.NullInt64
		channel           string
		provider          sql.NullString
		providerMessageID sql.NullString
		errorMessage      sql.NullString
		sentAt            sql.NullTime
		deliveredAt       sql.NullTime
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
		&errorMessage,
		&attempt.RequestedAt,
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
	attempt.ErrorMessage = sqlutil.StringPtr(errorMessage)
	attempt.SentAt = sqlutil.TimePtr(sentAt)
	attempt.DeliveredAt = sqlutil.TimePtr(deliveredAt)
	return &attempt, nil
}

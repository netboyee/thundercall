package notifications

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

func (r *Repository) Create(ctx context.Context, notification *models.Notification) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO notifications (
			nws_event_id, user_id, channel, first_message_id, last_message_id,
			status, first_attempted_at, sent_at, delivered_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		notification.NWSEventID,
		notification.UserID,
		string(notification.Channel),
		notification.FirstMessageID,
		notification.LastMessageID,
		notification.Status,
		sqlutil.TimeValue(notification.FirstAttemptedAt),
		sqlutil.TimeValue(notification.SentAt),
		sqlutil.TimeValue(notification.DeliveredAt),
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	notification.ID = id
	return nil
}

func (r *Repository) GetByEventUserChannel(ctx context.Context, eventID int64, userID int64, channel models.Channel) (*models.Notification, error) {
	row := r.db.QueryRowContext(
		ctx,
		selectNotificationSQL()+` WHERE nws_event_id = ? AND user_id = ? AND channel = ?`,
		eventID,
		userID,
		string(channel),
	)
	return scanNotification(row)
}

func (r *Repository) CreateOrGet(ctx context.Context, notification *models.Notification) (bool, error) {
	if err := r.Create(ctx, notification); err != nil {
		if !sqlutil.IsDuplicateKey(err) {
			return false, err
		}

		existing, getErr := r.GetByEventUserChannel(ctx, notification.NWSEventID, notification.UserID, notification.Channel)
		if getErr != nil {
			return false, getErr
		}
		if existing == nil {
			return false, err
		}

		*notification = *existing
		return false, nil
	}

	return true, nil
}

func (r *Repository) TouchLastMessage(ctx context.Context, id int64, lastMessageID int64) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE notifications
		 SET last_message_id = GREATEST(last_message_id, ?),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		lastMessageID,
		id,
	)
	return err
}

func (r *Repository) UpdateStatus(ctx context.Context, id int64, status string, lastMessageID int64, firstAttemptedAt *time.Time, sentAt *time.Time, deliveredAt *time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE notifications
		 SET status = ?,
		     last_message_id = GREATEST(last_message_id, ?),
		     first_attempted_at = COALESCE(first_attempted_at, ?),
		     sent_at = COALESCE(sent_at, ?),
		     delivered_at = COALESCE(delivered_at, ?),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		status,
		lastMessageID,
		sqlutil.TimeValue(firstAttemptedAt),
		sqlutil.TimeValue(sentAt),
		sqlutil.TimeValue(deliveredAt),
		id,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func selectNotificationSQL() string {
	return `
		SELECT
			id,
			nws_event_id,
			user_id,
			channel,
			first_message_id,
			last_message_id,
			status,
			first_attempted_at,
			sent_at,
			delivered_at,
			created_at,
			updated_at
		FROM notifications`
}

func scanNotification(s scanner) (*models.Notification, error) {
	var (
		notification     models.Notification
		channel          string
		firstAttemptedAt sql.NullTime
		sentAt           sql.NullTime
		deliveredAt      sql.NullTime
	)

	if err := s.Scan(
		&notification.ID,
		&notification.NWSEventID,
		&notification.UserID,
		&channel,
		&notification.FirstMessageID,
		&notification.LastMessageID,
		&notification.Status,
		&firstAttemptedAt,
		&sentAt,
		&deliveredAt,
		&notification.CreatedAt,
		&notification.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	notification.Channel = models.Channel(channel)
	notification.FirstAttemptedAt = sqlutil.TimePtr(firstAttemptedAt)
	notification.SentAt = sqlutil.TimePtr(sentAt)
	notification.DeliveredAt = sqlutil.TimePtr(deliveredAt)
	return &notification, nil
}

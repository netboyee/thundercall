package usersmessages

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

func (r *Repository) Create(ctx context.Context, userMessage *models.UserMessage) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users_messages (
			message_id, user_id, matched_location_id, resolution_reason,
			voice_enabled, status, queued_at, delivered_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userMessage.MessageID,
		userMessage.UserID,
		sqlutil.Int64Value(userMessage.MatchedLocationID),
		userMessage.ResolutionReason,
		userMessage.VoiceEnabled,
		userMessage.Status,
		userMessage.QueuedAt,
		sqlutil.TimeValue(userMessage.DeliveredAt),
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	userMessage.ID = id
	return nil
}

func (r *Repository) GetByMessageIDAndUserID(ctx context.Context, messageID int64, userID int64) (*models.UserMessage, error) {
	row := r.db.QueryRowContext(ctx, selectUserMessageSQL()+` WHERE message_id = ? AND user_id = ?`, messageID, userID)
	return scanUserMessage(row)
}

func (r *Repository) UpdateStatus(ctx context.Context, id int64, status string, deliveredAt *time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE users_messages
		 SET status = ?, delivered_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		status,
		sqlutil.TimeValue(deliveredAt),
		id,
	)
	return err
}

func (r *Repository) ListByMessageID(ctx context.Context, messageID int64) ([]models.UserMessage, error) {
	rows, err := r.db.QueryContext(ctx, selectUserMessageSQL()+`
	 WHERE message_id = ?
	 ORDER BY id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userMessages []models.UserMessage
	for rows.Next() {
		userMessage, err := scanUserMessage(rows)
		if err != nil {
			return nil, err
		}
		userMessages = append(userMessages, *userMessage)
	}

	return userMessages, rows.Err()
}

func (r *Repository) CreateOrGet(ctx context.Context, userMessage *models.UserMessage) error {
	if err := r.Create(ctx, userMessage); err != nil {
		if !sqlutil.IsDuplicateKey(err) {
			return err
		}

		existing, getErr := r.GetByMessageIDAndUserID(ctx, userMessage.MessageID, userMessage.UserID)
		if getErr != nil {
			return getErr
		}
		if existing == nil {
			return err
		}

		*userMessage = *existing
		return nil
	}

	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func selectUserMessageSQL() string {
	return `
		SELECT id, message_id, user_id, matched_location_id, resolution_reason,
		       FALSE AS sms_enabled, FALSE AS email_enabled, voice_enabled, status, queued_at,
		       delivered_at, created_at, updated_at
		FROM users_messages`
}

func scanUserMessage(s scanner) (*models.UserMessage, error) {
	var (
		userMessage       models.UserMessage
		matchedLocationID sql.NullInt64
		deliveredAt       sql.NullTime
	)

	if err := s.Scan(
		&userMessage.ID,
		&userMessage.MessageID,
		&userMessage.UserID,
		&matchedLocationID,
		&userMessage.ResolutionReason,
		&userMessage.SMSEnabled,
		&userMessage.EmailEnabled,
		&userMessage.VoiceEnabled,
		&userMessage.Status,
		&userMessage.QueuedAt,
		&deliveredAt,
		&userMessage.CreatedAt,
		&userMessage.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	userMessage.MatchedLocationID = sqlutil.Int64Ptr(matchedLocationID)
	userMessage.DeliveredAt = sqlutil.TimePtr(deliveredAt)
	return &userMessage, nil
}

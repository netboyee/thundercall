package usersettings

import (
	"context"
	"database/sql"
	"fmt"

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

func (r *Repository) Upsert(ctx context.Context, setting *models.UserSetting) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users_settings (user_id, message_type_code, voice_enabled)
		 VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   voice_enabled = VALUES(voice_enabled),
		   updated_at = CURRENT_TIMESTAMP`,
		setting.UserID,
		setting.MessageTypeCode,
		setting.VoiceEnabled,
	)
	if err != nil {
		return err
	}

	if id, err := result.LastInsertId(); err == nil && id > 0 {
		setting.ID = id
	}
	return nil
}

func (r *Repository) GetByUserAndMessageType(ctx context.Context, userID int64, messageTypeCode string) (*models.UserSetting, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, message_type_code, FALSE AS sms_enabled, FALSE AS email_enabled, voice_enabled, created_at, updated_at
		FROM users_settings
		WHERE user_id = ? AND message_type_code = ?`,
		userID,
		messageTypeCode,
	)
	return scanSetting(row)
}

func (r *Repository) ListByUserIDsAndMessageType(ctx context.Context, userIDs []int64, messageTypeCode string) ([]models.UserSetting, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(userIDs)+1)
	for _, id := range userIDs {
		args = append(args, id)
	}
	args = append(args, messageTypeCode)

	query := fmt.Sprintf(`
		SELECT id, user_id, message_type_code, FALSE AS sms_enabled, FALSE AS email_enabled, voice_enabled, created_at, updated_at
		FROM users_settings
		WHERE user_id IN (%s)
		  AND message_type_code = ?`, sqlutil.Placeholders(len(userIDs)))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []models.UserSetting
	for rows.Next() {
		setting, err := scanSetting(rows)
		if err != nil {
			return nil, err
		}
		settings = append(settings, *setting)
	}

	return settings, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSetting(s scanner) (*models.UserSetting, error) {
	var setting models.UserSetting
	if err := s.Scan(
		&setting.ID,
		&setting.UserID,
		&setting.MessageTypeCode,
		&setting.SMSEnabled,
		&setting.EmailEnabled,
		&setting.VoiceEnabled,
		&setting.CreatedAt,
		&setting.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &setting, nil
}

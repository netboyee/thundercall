package accountsettings

import (
	"context"
	"database/sql"
	"fmt"

	"thundercall-go/internal/models"
	"thundercall-go/internal/repositories/sqlutil"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Upsert(ctx context.Context, setting *models.AccountSetting) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO account_settings (account_id, message_type_code, voice_enabled)
		 VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   voice_enabled = VALUES(voice_enabled),
		   updated_at = CURRENT_TIMESTAMP`,
		setting.AccountID,
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

func (r *Repository) GetByAccountAndMessageType(ctx context.Context, accountID int64, messageTypeCode string) (*models.AccountSetting, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, message_type_code, FALSE AS sms_enabled, FALSE AS email_enabled, voice_enabled, created_at, updated_at
		FROM account_settings
		WHERE account_id = ? AND message_type_code = ?`,
		accountID,
		messageTypeCode,
	)
	return scanSetting(row)
}

func (r *Repository) ListByAccountIDsAndMessageType(ctx context.Context, accountIDs []int64, messageTypeCode string) ([]models.AccountSetting, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(accountIDs)+1)
	for _, id := range accountIDs {
		args = append(args, id)
	}
	args = append(args, messageTypeCode)

	query := fmt.Sprintf(`
		SELECT id, account_id, message_type_code, FALSE AS sms_enabled, FALSE AS email_enabled, voice_enabled, created_at, updated_at
		FROM account_settings
		WHERE account_id IN (%s)
		  AND message_type_code = ?`, sqlutil.Placeholders(len(accountIDs)))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []models.AccountSetting
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

func scanSetting(s scanner) (*models.AccountSetting, error) {
	var setting models.AccountSetting
	if err := s.Scan(
		&setting.ID,
		&setting.AccountID,
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

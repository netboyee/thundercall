package sourcemessages

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

func (r *Repository) Create(ctx context.Context, message *models.SourceMessage) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO source_messages (
			source, external_id, wmo_code, wfo_code, awips_id, product_category,
			issued_at, raw_payload, status, parse_error, received_at, parsed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.Source,
		message.ExternalID,
		sqlutil.StringValue(message.WMOCode),
		sqlutil.StringValue(message.WFOCode),
		sqlutil.StringValue(message.AWIPSID),
		sqlutil.StringValue(message.ProductCategory),
		sqlutil.TimeValue(message.IssuedAt),
		message.RawPayload,
		message.Status,
		sqlutil.StringValue(message.ParseError),
		message.ReceivedAt,
		sqlutil.TimeValue(message.ParsedAt),
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	message.ID = id
	return nil
}

func (r *Repository) GetBySourceAndExternalID(ctx context.Context, source string, externalID string) (*models.SourceMessage, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, source, external_id, wmo_code, wfo_code, awips_id, product_category,
		       issued_at, raw_payload, status, parse_error, received_at, parsed_at,
		       created_at, updated_at
		FROM source_messages
		WHERE source = ? AND external_id = ?`, source, externalID)
	return scanSourceMessage(row)
}

func (r *Repository) UpdateStatus(ctx context.Context, id int64, status string, parseError *string, parsedAt *time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE source_messages
		 SET status = ?, parse_error = ?, parsed_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		status,
		sqlutil.StringValue(parseError),
		sqlutil.TimeValue(parsedAt),
		id,
	)
	return err
}

func scanSourceMessage(s scanner) (*models.SourceMessage, error) {
	var (
		message         models.SourceMessage
		wmoCode         sql.NullString
		wfoCode         sql.NullString
		awipsID         sql.NullString
		productCategory sql.NullString
		issuedAt        sql.NullTime
		parseError      sql.NullString
		parsedAt        sql.NullTime
	)

	err := s.Scan(
		&message.ID,
		&message.Source,
		&message.ExternalID,
		&wmoCode,
		&wfoCode,
		&awipsID,
		&productCategory,
		&issuedAt,
		&message.RawPayload,
		&message.Status,
		&parseError,
		&message.ReceivedAt,
		&parsedAt,
		&message.CreatedAt,
		&message.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	message.WMOCode = sqlutil.StringPtr(wmoCode)
	message.WFOCode = sqlutil.StringPtr(wfoCode)
	message.AWIPSID = sqlutil.StringPtr(awipsID)
	message.ProductCategory = sqlutil.StringPtr(productCategory)
	message.IssuedAt = sqlutil.TimePtr(issuedAt)
	message.ParseError = sqlutil.StringPtr(parseError)
	message.ParsedAt = sqlutil.TimePtr(parsedAt)
	return &message, nil
}

type scanner interface {
	Scan(dest ...any) error
}

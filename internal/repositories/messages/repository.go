package messages

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

func (r *Repository) Create(ctx context.Context, message *models.Message) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO messages (
			account_id, source_message_id, nws_event_id, external_message_id, source_segment_index,
			fingerprint, source, event_code, message_type,
			alert_type_code, title, body, coordinate, polygon_wkt, fips_codes_json,
			nws_zones_json, primary_vtec_raw, vtec_action, original_payload, status, issued_at, received_at, processed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sqlutil.Int64Value(message.AccountID),
		sqlutil.Int64Value(message.SourceMessageID),
		sqlutil.Int64Value(message.NWSEventID),
		sqlutil.StringValue(message.ExternalMessageID),
		sqlutil.IntValue(message.SourceSegmentIndex),
		message.Fingerprint,
		message.Source,
		message.EventCode,
		message.MessageType,
		message.AlertTypeCode,
		sqlutil.StringValue(message.Title),
		message.Body,
		sqlutil.StringValue(message.Coordinate),
		sqlutil.StringValue(message.PolygonWKT),
		message.FIPSCodes,
		message.NWSZones,
		sqlutil.StringValue(message.PrimaryVTECRaw),
		sqlutil.StringValue(message.VTECAction),
		sqlutil.StringValue(message.OriginalPayload),
		message.Status,
		sqlutil.TimeValue(message.IssuedAt),
		message.ReceivedAt,
		sqlutil.TimeValue(message.ProcessedAt),
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

func (r *Repository) GetByID(ctx context.Context, id int64) (*models.Message, error) {
	row := r.db.QueryRowContext(ctx, selectMessageSQL()+` WHERE id = ?`, id)
	return scanMessage(row)
}

func (r *Repository) UpdateStatus(ctx context.Context, id int64, status string, processedAt *time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE messages
		 SET status = ?, processed_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		status,
		sqlutil.TimeValue(processedAt),
		id,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func selectMessageSQL() string {
	return `
		SELECT
			id,
			account_id,
			source_message_id,
			nws_event_id,
			external_message_id,
			source_segment_index,
			fingerprint,
			source,
			event_code,
			message_type,
			alert_type_code,
			title,
			body,
			coordinate,
			polygon_wkt,
			fips_codes_json,
			nws_zones_json,
			primary_vtec_raw,
			vtec_action,
			original_payload,
			status,
			issued_at,
			received_at,
			processed_at,
			created_at,
			updated_at
		FROM messages`
}

func scanMessage(s scanner) (*models.Message, error) {
	var (
		message            models.Message
		accountID          sql.NullInt64
		sourceMessageID    sql.NullInt64
		nwsEventID         sql.NullInt64
		externalMessageID  sql.NullString
		sourceSegmentIndex sql.NullInt64
		title              sql.NullString
		coordinate         sql.NullString
		polygonWKT         sql.NullString
		primaryVTECRaw     sql.NullString
		vtecAction         sql.NullString
		originalPayload    sql.NullString
		issuedAt           sql.NullTime
		processedAt        sql.NullTime
	)

	err := s.Scan(
		&message.ID,
		&accountID,
		&sourceMessageID,
		&nwsEventID,
		&externalMessageID,
		&sourceSegmentIndex,
		&message.Fingerprint,
		&message.Source,
		&message.EventCode,
		&message.MessageType,
		&message.AlertTypeCode,
		&title,
		&message.Body,
		&coordinate,
		&polygonWKT,
		&message.FIPSCodes,
		&message.NWSZones,
		&primaryVTECRaw,
		&vtecAction,
		&originalPayload,
		&message.Status,
		&issuedAt,
		&message.ReceivedAt,
		&processedAt,
		&message.CreatedAt,
		&message.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	message.AccountID = sqlutil.Int64Ptr(accountID)
	message.SourceMessageID = sqlutil.Int64Ptr(sourceMessageID)
	message.NWSEventID = sqlutil.Int64Ptr(nwsEventID)
	message.ExternalMessageID = sqlutil.StringPtr(externalMessageID)
	message.SourceSegmentIndex = sqlutil.IntPtr[int](sourceSegmentIndex)
	message.Title = sqlutil.StringPtr(title)
	message.Coordinate = sqlutil.StringPtr(coordinate)
	message.PolygonWKT = sqlutil.StringPtr(polygonWKT)
	message.PrimaryVTECRaw = sqlutil.StringPtr(primaryVTECRaw)
	message.VTECAction = sqlutil.StringPtr(vtecAction)
	message.OriginalPayload = sqlutil.StringPtr(originalPayload)
	message.IssuedAt = sqlutil.TimePtr(issuedAt)
	message.ProcessedAt = sqlutil.TimePtr(processedAt)
	return &message, nil
}

package nwsevents

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

func (r *Repository) Create(ctx context.Context, event *models.NWSEvent) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO nws_events (
			event_key, product_class, office_id, phenomenon, significance, etn, event_year,
			last_action, begins_at, ends_at, first_issued_at, last_issued_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventKey,
		event.ProductClass,
		event.OfficeID,
		event.Phenomenon,
		event.Significance,
		event.ETN,
		event.EventYear,
		event.LastAction,
		sqlutil.TimeValue(event.BeginsAt),
		sqlutil.TimeValue(event.EndsAt),
		sqlutil.TimeValue(event.FirstIssuedAt),
		sqlutil.TimeValue(event.LastIssuedAt),
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	event.ID = id
	return nil
}

func (r *Repository) GetByEventKey(ctx context.Context, eventKey string) (*models.NWSEvent, error) {
	row := r.db.QueryRowContext(ctx, selectNWSEventSQL()+` WHERE event_key = ?`, eventKey)
	return scanNWSEvent(row)
}

func (r *Repository) GetByNaturalKey(ctx context.Context, productClass string, officeID string, phenomenon string, significance string, etn string, eventYear int) (*models.NWSEvent, error) {
	row := r.db.QueryRowContext(
		ctx,
		selectNWSEventSQL()+`
		WHERE product_class = ?
		  AND office_id = ?
		  AND phenomenon = ?
		  AND significance = ?
		  AND etn = ?
		  AND event_year = ?`,
		productClass,
		officeID,
		phenomenon,
		significance,
		etn,
		eventYear,
	)
	return scanNWSEvent(row)
}

func (r *Repository) CreateOrGet(ctx context.Context, event *models.NWSEvent) (bool, error) {
	if err := r.Create(ctx, event); err != nil {
		if !sqlutil.IsDuplicateKey(err) {
			return false, err
		}

		existing, getErr := r.GetByEventKey(ctx, event.EventKey)
		if getErr != nil {
			return false, getErr
		}
		if existing == nil {
			return false, err
		}

		*event = *existing
		return false, nil
	}

	return true, nil
}

func (r *Repository) UpdateLifecycle(ctx context.Context, id int64, lastAction string, beginsAt *time.Time, endsAt *time.Time, issuedAt *time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE nws_events
		 SET last_action = ?,
		     begins_at = COALESCE(begins_at, ?),
		     ends_at = ?,
		     first_issued_at = COALESCE(first_issued_at, ?),
		     last_issued_at = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		lastAction,
		sqlutil.TimeValue(beginsAt),
		sqlutil.TimeValue(endsAt),
		sqlutil.TimeValue(issuedAt),
		sqlutil.TimeValue(issuedAt),
		id,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func selectNWSEventSQL() string {
	return `
		SELECT
			id,
			event_key,
			product_class,
			office_id,
			phenomenon,
			significance,
			etn,
			event_year,
			last_action,
			begins_at,
			ends_at,
			first_issued_at,
			last_issued_at,
			created_at,
			updated_at
		FROM nws_events`
}

func scanNWSEvent(s scanner) (*models.NWSEvent, error) {
	var (
		event         models.NWSEvent
		beginsAt      sql.NullTime
		endsAt        sql.NullTime
		firstIssuedAt sql.NullTime
		lastIssuedAt  sql.NullTime
	)

	if err := s.Scan(
		&event.ID,
		&event.EventKey,
		&event.ProductClass,
		&event.OfficeID,
		&event.Phenomenon,
		&event.Significance,
		&event.ETN,
		&event.EventYear,
		&event.LastAction,
		&beginsAt,
		&endsAt,
		&firstIssuedAt,
		&lastIssuedAt,
		&event.CreatedAt,
		&event.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	event.BeginsAt = sqlutil.TimePtr(beginsAt)
	event.EndsAt = sqlutil.TimePtr(endsAt)
	event.FirstIssuedAt = sqlutil.TimePtr(firstIssuedAt)
	event.LastIssuedAt = sqlutil.TimePtr(lastIssuedAt)
	return &event, nil
}

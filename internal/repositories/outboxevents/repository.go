package outboxevents

import (
	"context"
	"database/sql"
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

func (r *Repository) Create(ctx context.Context, event *models.OutboxEvent) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO outbox_events (
			aggregate_type, aggregate_id, event_type, stream_key, payload_json,
			published_at, attempt_count, last_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.AggregateType,
		event.AggregateID,
		event.EventType,
		event.StreamKey,
		event.PayloadJSON,
		sqlutil.TimeValue(event.PublishedAt),
		event.AttemptCount,
		sqlutil.StringValue(event.LastError),
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

func (r *Repository) ListUnpublished(ctx context.Context, limit int) ([]models.OutboxEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, aggregate_type, aggregate_id, event_type, stream_key, payload_json,
		       published_at, attempt_count, last_error, created_at, updated_at
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.OutboxEvent
	for rows.Next() {
		var (
			event       models.OutboxEvent
			publishedAt sql.NullTime
			lastError   sql.NullString
		)
		if err := rows.Scan(
			&event.ID,
			&event.AggregateType,
			&event.AggregateID,
			&event.EventType,
			&event.StreamKey,
			&event.PayloadJSON,
			&publishedAt,
			&event.AttemptCount,
			&lastError,
			&event.CreatedAt,
			&event.UpdatedAt,
		); err != nil {
			return nil, err
		}

		event.PublishedAt = sqlutil.TimePtr(publishedAt)
		event.LastError = sqlutil.StringPtr(lastError)
		events = append(events, event)
	}

	return events, rows.Err()
}

func (r *Repository) MarkPublished(ctx context.Context, id int64, publishedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE outbox_events
		 SET published_at = ?, attempt_count = attempt_count + 1, last_error = NULL, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		publishedAt,
		id,
	)
	return err
}

func (r *Repository) MarkFailed(ctx context.Context, id int64, lastError string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE outbox_events
		 SET attempt_count = attempt_count + 1, last_error = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		lastError,
		id,
	)
	return err
}

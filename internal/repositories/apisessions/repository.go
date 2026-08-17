package apisessions

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

func (r *Repository) Create(ctx context.Context, session *models.APISession) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO api_sessions (
			api_user_id, token_hash, expires_at, revoked_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?)`,
		session.APIUserID,
		session.TokenHash,
		session.ExpiresAt.UTC(),
		sqlutil.TimeValue(session.RevokedAt),
		sqlutil.TimeValue(session.LastSeenAt),
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	session.ID = id
	return nil
}

func (r *Repository) GetByTokenHash(ctx context.Context, tokenHash string) (*models.APISession, error) {
	row := r.db.QueryRowContext(ctx, selectAPISessionSQL()+` WHERE token_hash = ?`, tokenHash)
	return scanAPISession(row)
}

func (r *Repository) RevokeByTokenHash(ctx context.Context, tokenHash string, revokedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE api_sessions
		 SET revoked_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE token_hash = ?`,
		revokedAt.UTC(),
		tokenHash,
	)
	return err
}

func (r *Repository) TouchLastSeen(ctx context.Context, id int64, when time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE api_sessions
		 SET last_seen_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		when.UTC(),
		id,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func selectAPISessionSQL() string {
	return `
		SELECT id, api_user_id, token_hash, expires_at, revoked_at, last_seen_at, created_at, updated_at
		FROM api_sessions`
}

func scanAPISession(s scanner) (*models.APISession, error) {
	var (
		session    models.APISession
		revokedAt  sql.NullTime
		lastSeenAt sql.NullTime
	)

	err := s.Scan(
		&session.ID,
		&session.APIUserID,
		&session.TokenHash,
		&session.ExpiresAt,
		&revokedAt,
		&lastSeenAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	session.RevokedAt = sqlutil.TimePtr(revokedAt)
	session.LastSeenAt = sqlutil.TimePtr(lastSeenAt)
	return &session, nil
}

package apiusers

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

func (r *Repository) Create(ctx context.Context, user *models.APIUser) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO api_users (
			account_id, email, password_hash, display_name, active, last_login_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		user.AccountID,
		normalizeEmail(user.Email),
		user.PasswordHash,
		sqlutil.StringValue(user.DisplayName),
		user.Active,
		sqlutil.TimeValue(user.LastLoginAt),
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	user.ID = id
	return nil
}

func (r *Repository) GetByEmail(ctx context.Context, email string) (*models.APIUser, error) {
	row := r.db.QueryRowContext(ctx, selectAPIUserSQL()+` WHERE email = ?`, normalizeEmail(email))
	return scanAPIUser(row)
}

func (r *Repository) GetByID(ctx context.Context, id int64) (*models.APIUser, error) {
	row := r.db.QueryRowContext(ctx, selectAPIUserSQL()+` WHERE id = ?`, id)
	return scanAPIUser(row)
}

func (r *Repository) UpdateLastLogin(ctx context.Context, id int64, when time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE api_users
		 SET last_login_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		when.UTC(),
		id,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func selectAPIUserSQL() string {
	return `
		SELECT id, account_id, email, password_hash, display_name, active, last_login_at, created_at, updated_at
		FROM api_users`
}

func scanAPIUser(s scanner) (*models.APIUser, error) {
	var (
		user        models.APIUser
		displayName sql.NullString
		lastLoginAt sql.NullTime
	)

	err := s.Scan(
		&user.ID,
		&user.AccountID,
		&user.Email,
		&user.PasswordHash,
		&displayName,
		&user.Active,
		&lastLoginAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	user.DisplayName = sqlutil.StringPtr(displayName)
	user.LastLoginAt = sqlutil.TimePtr(lastLoginAt)
	return &user, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

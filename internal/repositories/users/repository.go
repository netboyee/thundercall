package users

import (
	"context"
	"database/sql"
	"errors"
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

func (r *Repository) Create(ctx context.Context, user *models.User) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users (account_id, legacy_record_id, external_id, first_name, last_name, display_name, title, active)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.AccountID,
		sqlutil.Int64Value(user.LegacyRecordID),
		sqlutil.StringValue(user.ExternalID),
		sqlutil.StringValue(user.FirstName),
		sqlutil.StringValue(user.LastName),
		sqlutil.StringValue(user.DisplayName),
		sqlutil.StringValue(user.Title),
		user.Active,
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

func (r *Repository) GetByID(ctx context.Context, id int64) (*models.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, legacy_record_id, external_id, first_name, last_name, display_name, title, active, created_at, updated_at
		FROM users
		WHERE id = ?`,
		id,
	)
	return scanUser(row)
}

func (r *Repository) ListByAccountID(ctx context.Context, accountID int64) ([]models.User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, legacy_record_id, external_id, first_name, last_name, display_name, title, active, created_at, updated_at
		FROM users
		WHERE account_id = ?
		ORDER BY id`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}

	return users, rows.Err()
}

func (r *Repository) ListByIDs(ctx context.Context, ids []int64) ([]models.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, account_id, legacy_record_id, external_id, first_name, last_name, display_name, title, active, created_at, updated_at
		FROM users
		WHERE id IN (%s)
		ORDER BY id`, sqlutil.Placeholders(len(ids)))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}

	return users, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*models.User, error) {
	var (
		user           models.User
		legacyRecordID sql.NullInt64
		externalID     sql.NullString
		firstName      sql.NullString
		lastName       sql.NullString
		displayName    sql.NullString
		title          sql.NullString
	)

	err := s.Scan(
		&user.ID,
		&user.AccountID,
		&legacyRecordID,
		&externalID,
		&firstName,
		&lastName,
		&displayName,
		&title,
		&user.Active,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	user.LegacyRecordID = sqlutil.Int64Ptr(legacyRecordID)
	user.ExternalID = sqlutil.StringPtr(externalID)
	user.FirstName = sqlutil.StringPtr(firstName)
	user.LastName = sqlutil.StringPtr(lastName)
	user.DisplayName = sqlutil.StringPtr(displayName)
	user.Title = sqlutil.StringPtr(title)
	return &user, nil
}

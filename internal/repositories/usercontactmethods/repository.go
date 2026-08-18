package usercontactmethods

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

func (r *Repository) Create(ctx context.Context, method *models.UserContactMethod) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO user_contact_methods (user_id, channel, destination, is_primary, is_verified, active)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		method.UserID,
		string(method.Channel),
		method.Destination,
		method.IsPrimary,
		method.IsVerified,
		method.Active,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	method.ID = id
	return nil
}

func (r *Repository) Upsert(ctx context.Context, method *models.UserContactMethod) error {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id
		 FROM user_contact_methods
		 WHERE user_id = ? AND channel = ? AND destination = ?
		 LIMIT 1`,
		method.UserID,
		string(method.Channel),
		method.Destination,
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r.Create(ctx, method)
		}
		return err
	}

	method.ID = id
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE user_contact_methods
		 SET is_primary = ?, is_verified = ?, active = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		method.IsPrimary,
		method.IsVerified,
		method.Active,
		method.ID,
	)
	return err
}

func (r *Repository) FindActiveUserIDByAccountAndChannelDestination(ctx context.Context, accountID int64, channel models.Channel, destination string) (int64, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT u.id
		 FROM user_contact_methods ucm
		 INNER JOIN users u
		   ON u.id = ucm.user_id
		 WHERE u.account_id = ?
		   AND u.active = 1
		   AND ucm.active = 1
		   AND ucm.channel = ?
		   AND ucm.destination = ?
		 ORDER BY u.id
		 LIMIT 1`,
		accountID,
		string(channel),
		destination,
	)

	var userID int64
	if err := row.Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return userID, nil
}

func (r *Repository) ListByUserID(ctx context.Context, userID int64) ([]models.UserContactMethod, error) {
	return r.ListByUserIDs(ctx, []int64{userID})
}

func (r *Repository) ListByUserIDs(ctx context.Context, userIDs []int64) ([]models.UserContactMethod, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, user_id, channel, destination, is_primary, is_verified, active, created_at, updated_at
		FROM user_contact_methods
		WHERE user_id IN (%s)
		  AND active = 1
		ORDER BY user_id, is_primary DESC, id`, sqlutil.Placeholders(len(userIDs)))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var methods []models.UserContactMethod
	for rows.Next() {
		var method models.UserContactMethod
		var channel string

		if err := rows.Scan(
			&method.ID,
			&method.UserID,
			&channel,
			&method.Destination,
			&method.IsPrimary,
			&method.IsVerified,
			&method.Active,
			&method.CreatedAt,
			&method.UpdatedAt,
		); err != nil {
			return nil, err
		}

		method.Channel = models.Channel(channel)
		methods = append(methods, method)
	}

	return methods, rows.Err()
}

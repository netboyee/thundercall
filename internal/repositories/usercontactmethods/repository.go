package usercontactmethods

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

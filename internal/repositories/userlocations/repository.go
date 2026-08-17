package userlocations

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

func (r *Repository) Create(ctx context.Context, userLocation *models.UserLocation) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users_locations (user_id, location_id, subscription_type, is_primary, is_thundercall_enabled)
		 VALUES (?, ?, ?, ?, ?)`,
		userLocation.UserID,
		userLocation.LocationID,
		userLocation.SubscriptionType,
		userLocation.IsPrimary,
		userLocation.IsThunderCallEnabled,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	userLocation.ID = id
	return nil
}

func (r *Repository) ListByLocationIDs(ctx context.Context, locationIDs []int64) ([]models.UserLocation, error) {
	if len(locationIDs) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(locationIDs))
	for _, id := range locationIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, user_id, location_id, subscription_type, is_primary, is_thundercall_enabled, created_at, updated_at
		FROM users_locations
		WHERE location_id IN (%s)
		ORDER BY user_id, is_primary DESC, id`, sqlutil.Placeholders(len(locationIDs)))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userLocations []models.UserLocation
	for rows.Next() {
		var userLocation models.UserLocation
		if err := rows.Scan(
			&userLocation.ID,
			&userLocation.UserID,
			&userLocation.LocationID,
			&userLocation.SubscriptionType,
			&userLocation.IsPrimary,
			&userLocation.IsThunderCallEnabled,
			&userLocation.CreatedAt,
			&userLocation.UpdatedAt,
		); err != nil {
			return nil, err
		}
		userLocations = append(userLocations, userLocation)
	}

	return userLocations, rows.Err()
}

func (r *Repository) ListByUserID(ctx context.Context, userID int64) ([]models.UserLocation, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, location_id, subscription_type, is_primary, is_thundercall_enabled, created_at, updated_at
		FROM users_locations
		WHERE user_id = ?
		ORDER BY is_primary DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userLocations []models.UserLocation
	for rows.Next() {
		var userLocation models.UserLocation
		if err := rows.Scan(
			&userLocation.ID,
			&userLocation.UserID,
			&userLocation.LocationID,
			&userLocation.SubscriptionType,
			&userLocation.IsPrimary,
			&userLocation.IsThunderCallEnabled,
			&userLocation.CreatedAt,
			&userLocation.UpdatedAt,
		); err != nil {
			return nil, err
		}
		userLocations = append(userLocations, userLocation)
	}

	return userLocations, rows.Err()
}

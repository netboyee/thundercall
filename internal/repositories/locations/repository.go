package locations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

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

func (r *Repository) Create(ctx context.Context, location *models.Location) error {
	var (
		result sql.Result
		err    error
	)

	if location.CoverageWKT != nil {
		result, err = r.db.ExecContext(
			ctx,
			`INSERT INTO locations (
				account_id, name,
				address_line_1, address_line_2, city, state_code, postal_code,
				county_fips, nws_zone, latitude, longitude, coverage_geometry,
				is_thundercall_enabled, active
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ST_GeomFromText(?, 4326), ?, ?)`,
			location.AccountID,
			location.Name,
			sqlutil.StringValue(location.AddressLine1),
			sqlutil.StringValue(location.AddressLine2),
			sqlutil.StringValue(location.City),
			sqlutil.StringValue(location.StateCode),
			sqlutil.StringValue(location.PostalCode),
			sqlutil.StringValue(location.CountyFIPS),
			sqlutil.StringValue(location.NWSZone),
			sqlutil.Float64Value(location.Latitude),
			sqlutil.Float64Value(location.Longitude),
			*location.CoverageWKT,
			location.IsThunderCallEnabled,
			location.Active,
		)
	} else {
		result, err = r.db.ExecContext(
			ctx,
			`INSERT INTO locations (
				account_id, name,
				address_line_1, address_line_2, city, state_code, postal_code,
				county_fips, nws_zone, latitude, longitude, coverage_geometry,
				is_thundercall_enabled, active
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
			location.AccountID,
			location.Name,
			sqlutil.StringValue(location.AddressLine1),
			sqlutil.StringValue(location.AddressLine2),
			sqlutil.StringValue(location.City),
			sqlutil.StringValue(location.StateCode),
			sqlutil.StringValue(location.PostalCode),
			sqlutil.StringValue(location.CountyFIPS),
			sqlutil.StringValue(location.NWSZone),
			sqlutil.Float64Value(location.Latitude),
			sqlutil.Float64Value(location.Longitude),
			location.IsThunderCallEnabled,
			location.Active,
		)
	}
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	location.ID = id
	return nil
}

func (r *Repository) Update(ctx context.Context, location *models.Location) error {
	if location.CoverageWKT != nil {
		_, err := r.db.ExecContext(
			ctx,
			`UPDATE locations
			 SET name = ?,
			     address_line_1 = ?, address_line_2 = ?, city = ?, state_code = ?, postal_code = ?,
			     county_fips = ?, nws_zone = ?, latitude = ?, longitude = ?, coverage_geometry = ST_GeomFromText(?, 4326),
			     is_thundercall_enabled = ?, active = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			location.Name,
			sqlutil.StringValue(location.AddressLine1),
			sqlutil.StringValue(location.AddressLine2),
			sqlutil.StringValue(location.City),
			sqlutil.StringValue(location.StateCode),
			sqlutil.StringValue(location.PostalCode),
			sqlutil.StringValue(location.CountyFIPS),
			sqlutil.StringValue(location.NWSZone),
			sqlutil.Float64Value(location.Latitude),
			sqlutil.Float64Value(location.Longitude),
			*location.CoverageWKT,
			location.IsThunderCallEnabled,
			location.Active,
			location.ID,
		)
		return err
	}

	_, err := r.db.ExecContext(
		ctx,
		`UPDATE locations
		 SET name = ?,
		     address_line_1 = ?, address_line_2 = ?, city = ?, state_code = ?, postal_code = ?,
		     county_fips = ?, nws_zone = ?, latitude = ?, longitude = ?, coverage_geometry = NULL,
		     is_thundercall_enabled = ?, active = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		location.Name,
		sqlutil.StringValue(location.AddressLine1),
		sqlutil.StringValue(location.AddressLine2),
		sqlutil.StringValue(location.City),
		sqlutil.StringValue(location.StateCode),
		sqlutil.StringValue(location.PostalCode),
		sqlutil.StringValue(location.CountyFIPS),
		sqlutil.StringValue(location.NWSZone),
		sqlutil.Float64Value(location.Latitude),
		sqlutil.Float64Value(location.Longitude),
		location.IsThunderCallEnabled,
		location.Active,
		location.ID,
	)
	return err
}

func (r *Repository) GetByID(ctx context.Context, id int64) (*models.Location, error) {
	row := r.db.QueryRowContext(ctx, selectLocationSQL()+` WHERE id = ?`, id)
	return scanLocation(row)
}

func (r *Repository) ListByAccountID(ctx context.Context, accountID int64) ([]models.Location, error) {
	rows, err := r.db.QueryContext(ctx, selectLocationSQL()+` WHERE account_id = ? ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locations []models.Location
	for rows.Next() {
		location, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		locations = append(locations, *location)
	}

	return locations, rows.Err()
}

func (r *Repository) MatchForMessage(ctx context.Context, polygonWKT string, fipsCodes []string, nwsZones []string) ([]models.Location, error) {
	polygonClauses := make([]string, 0, 1)
	polygonArgs := make([]any, 0, 1)
	fallbackClauses := make([]string, 0, 2)
	fallbackArgs := make([]any, 0, len(fipsCodes)+len(nwsZones))

	if strings.TrimSpace(polygonWKT) != "" {
		polygonClauses = append(polygonClauses, `(coverage_geometry IS NOT NULL AND ST_Intersects(coverage_geometry, ST_GeomFromText(?, 4326)) = 1)`)
		polygonArgs = append(polygonArgs, polygonWKT)
	}

	if len(fipsCodes) > 0 {
		fallbackClauses = append(fallbackClauses, fmt.Sprintf(`county_fips IN (%s)`, sqlutil.Placeholders(len(fipsCodes))))
		for _, value := range fipsCodes {
			fallbackArgs = append(fallbackArgs, value)
		}
	}

	if len(nwsZones) > 0 {
		fallbackClauses = append(fallbackClauses, fmt.Sprintf(`nws_zone IN (%s)`, sqlutil.Placeholders(len(nwsZones))))
		for _, value := range nwsZones {
			fallbackArgs = append(fallbackArgs, value)
		}
	}

	clauses := append(append([]string{}, polygonClauses...), fallbackClauses...)
	args := append(append([]any{}, polygonArgs...), fallbackArgs...)
	if len(clauses) == 0 {
		return nil, nil
	}

	locations, err := r.matchWithClauses(ctx, clauses, args)
	if err == nil {
		return locations, nil
	}
	if !isInvalidGISError(err) || len(polygonClauses) == 0 {
		return nil, err
	}
	if len(fallbackClauses) == 0 {
		return nil, nil
	}
	log.Printf(
		"location match falling back to county_fips/nws_zone because polygon query failed: %v",
		err,
	)
	return r.matchWithClauses(ctx, fallbackClauses, fallbackArgs)
}

func (r *Repository) matchWithClauses(ctx context.Context, clauses []string, args []any) ([]models.Location, error) {
	query := selectLocationSQL() + `
		WHERE active = 1
		  AND is_thundercall_enabled = 1
		  AND (` + strings.Join(clauses, ` OR `) + `)
		ORDER BY account_id, id`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locations []models.Location
	for rows.Next() {
		location, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		locations = append(locations, *location)
	}

	return locations, rows.Err()
}

func isInvalidGISError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid gis data provided") || strings.Contains(message, "st_geomfromtext")
}

type scanner interface {
	Scan(dest ...any) error
}

func selectLocationSQL() string {
	return `
		SELECT
			id,
			account_id,
			name,
			address_line_1,
			address_line_2,
			city,
			state_code,
			postal_code,
			county_fips,
			nws_zone,
			latitude,
			longitude,
			ST_AsText(coverage_geometry) AS coverage_wkt,
			is_thundercall_enabled,
			active,
			created_at,
			updated_at
		FROM locations`
}

func scanLocation(s scanner) (*models.Location, error) {
	var (
		location     models.Location
		addressLine1 sql.NullString
		addressLine2 sql.NullString
		city         sql.NullString
		stateCode    sql.NullString
		postalCode   sql.NullString
		countyFIPS   sql.NullString
		nwsZone      sql.NullString
		latitude     sql.NullFloat64
		longitude    sql.NullFloat64
		coverageWKT  sql.NullString
	)

	err := s.Scan(
		&location.ID,
		&location.AccountID,
		&location.Name,
		&addressLine1,
		&addressLine2,
		&city,
		&stateCode,
		&postalCode,
		&countyFIPS,
		&nwsZone,
		&latitude,
		&longitude,
		&coverageWKT,
		&location.IsThunderCallEnabled,
		&location.Active,
		&location.CreatedAt,
		&location.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	location.AddressLine1 = sqlutil.StringPtr(addressLine1)
	location.AddressLine2 = sqlutil.StringPtr(addressLine2)
	location.City = sqlutil.StringPtr(city)
	location.StateCode = sqlutil.StringPtr(stateCode)
	location.PostalCode = sqlutil.StringPtr(postalCode)
	location.CountyFIPS = sqlutil.StringPtr(countyFIPS)
	location.NWSZone = sqlutil.StringPtr(nwsZone)
	location.Latitude = sqlutil.Float64Ptr(latitude)
	location.Longitude = sqlutil.Float64Ptr(longitude)
	location.CoverageWKT = sqlutil.StringPtr(coverageWKT)
	return &location, nil
}

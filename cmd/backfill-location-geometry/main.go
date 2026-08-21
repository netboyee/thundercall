// Command backfill-location-geometry enriches legacy locations that have an
// address and fallback routing data but no point geometry.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"thundercall-go/internal/config"
	"thundercall-go/internal/database"
	"thundercall-go/internal/geocode"
)

type options struct {
	accountID         int64
	apply             bool
	limit             int
	resumeAfterID     int64
	requestsPerSecond float64
	retries           int
	reportPath        string
}

type location struct {
	ID           int64
	AccountID    int64
	AddressLine  string
	AddressLine2 string
	City         string
	StateCode    string
	PostalCode   string
}

type reportRow struct {
	LocationID int64  `json:"locationId"`
	AccountID  int64  `json:"accountId"`
	Address    string `json:"address"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type report struct {
	DryRun   bool        `json:"dryRun"`
	Started  time.Time   `json:"startedAt"`
	Finished time.Time   `json:"finishedAt"`
	Rows     []reportRow `json:"rows"`
}

func main() {
	opts := parseOptions()
	if err := run(context.Background(), opts); err != nil {
		log.Fatal(err)
	}
}

func parseOptions() options {
	var opts options
	flag.Int64Var(&opts.accountID, "account-id", 0, "only process this account ID")
	flag.BoolVar(&opts.apply, "apply", false, "write successful enrichments (the default is dry-run)")
	flag.IntVar(&opts.limit, "limit", 0, "maximum locations to process (0 means no limit)")
	flag.Int64Var(&opts.resumeAfterID, "resume-after-id", 0, "only process location IDs greater than this value")
	flag.Float64Var(&opts.requestsPerSecond, "requests-per-second", 1, "maximum Census requests per second")
	flag.IntVar(&opts.retries, "retries", 3, "retries for transient geocoding failures")
	flag.StringVar(&opts.reportPath, "report", "", "write a JSON report to this path")
	flag.Parse()

	if opts.accountID < 0 {
		log.Fatal("--account-id must be positive")
	}
	if opts.limit < 0 {
		log.Fatal("--limit must not be negative")
	}
	if opts.resumeAfterID < 0 {
		log.Fatal("--resume-after-id must not be negative")
	}
	if opts.requestsPerSecond <= 0 || opts.requestsPerSecond > 5 {
		log.Fatal("--requests-per-second must be greater than 0 and no more than 5")
	}
	if opts.retries < 0 {
		log.Fatal("--retries must not be negative")
	}
	return opts
}

func run(ctx context.Context, opts options) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.MySQL.Enabled() {
		return errors.New("THUNDERCALL_MYSQL_DSN is required")
	}

	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()

	locations, err := loadLocations(ctx, db, opts)
	if err != nil {
		return err
	}

	mode := "dry-run"
	if opts.apply {
		mode = "apply"
	}
	log.Printf("mode=%s eligible_locations=%d account_id=%d resume_after_id=%d", mode, len(locations), opts.accountID, opts.resumeAfterID)

	resolver := geocode.New(cfg.Geocoding)
	result := report{DryRun: !opts.apply, Started: time.Now().UTC(), Rows: make([]reportRow, 0, len(locations))}
	interval := time.Duration(float64(time.Second) / opts.requestsPerSecond)
	nextRequestAt := time.Time{}

	for _, item := range locations {
		if wait := time.Until(nextRequestAt); wait > 0 {
			time.Sleep(wait)
		}
		nextRequestAt = time.Now().Add(interval)

		address := geocode.Address{
			Line1:      item.AddressLine,
			Line2:      item.AddressLine2,
			City:       item.City,
			StateCode:  item.StateCode,
			PostalCode: item.PostalCode,
		}
		resolved, err := resolveWithRetry(ctx, resolver, address, opts.retries)
		row := reportRow{LocationID: item.ID, AccountID: item.AccountID, Address: address.OneLine()}
		if err != nil {
			row.Status = "failed"
			row.Error = err.Error()
			result.Rows = append(result.Rows, row)
			log.Printf("location_id=%d account_id=%d status=failed error=%v", item.ID, item.AccountID, err)
			continue
		}

		if opts.apply {
			if err := updateLocation(ctx, db, item.ID, resolved); err != nil {
				row.Status = "failed"
				row.Error = err.Error()
				result.Rows = append(result.Rows, row)
				log.Printf("location_id=%d account_id=%d status=failed error=%v", item.ID, item.AccountID, err)
				continue
			}
			row.Status = "updated"
		} else {
			row.Status = "matched"
		}
		result.Rows = append(result.Rows, row)
		log.Printf("location_id=%d account_id=%d status=%s", item.ID, item.AccountID, row.Status)
	}

	result.Finished = time.Now().UTC()
	if err := writeReport(opts.reportPath, result); err != nil {
		return err
	}
	matched, updated, failed := summarize(result.Rows)
	log.Printf("complete mode=%s matched=%d updated=%d failed=%d", mode, matched, updated, failed)
	return nil
}

func loadLocations(ctx context.Context, db *sql.DB, opts options) ([]location, error) {
	clauses := []string{
		"coverage_geometry IS NULL",
		"address_line_1 IS NOT NULL AND TRIM(address_line_1) <> ''",
		"city IS NOT NULL AND TRIM(city) <> ''",
		"state_code IS NOT NULL AND TRIM(state_code) <> ''",
		"postal_code IS NOT NULL AND TRIM(postal_code) <> ''",
		"id > ?",
	}
	args := []any{opts.resumeAfterID}
	if opts.accountID != 0 {
		clauses = append(clauses, "account_id = ?")
		args = append(args, opts.accountID)
	}

	query := `SELECT id, account_id, address_line_1, COALESCE(address_line_2, ''), city, state_code, postal_code
		FROM locations WHERE ` + strings.Join(clauses, " AND ") + " ORDER BY id"
	if opts.limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load locations: %w", err)
	}
	defer rows.Close()

	items := make([]location, 0)
	for rows.Next() {
		var item location
		if err := rows.Scan(&item.ID, &item.AccountID, &item.AddressLine, &item.AddressLine2, &item.City, &item.StateCode, &item.PostalCode); err != nil {
			return nil, fmt.Errorf("scan location: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locations: %w", err)
	}
	return items, nil
}

func resolveWithRetry(ctx context.Context, resolver geocode.Resolver, address geocode.Address, retries int) (geocode.ResolvedLocation, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		resolved, err := resolver.ResolveAddress(ctx, address)
		if err == nil || errors.Is(err, geocode.ErrNoMatch) {
			return resolved, err
		}
		lastErr = err
		if attempt < retries {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}
	return geocode.ResolvedLocation{}, lastErr
}

func updateLocation(ctx context.Context, db *sql.DB, locationID int64, resolved geocode.ResolvedLocation) error {
	if resolved.Latitude == 0 && resolved.Longitude == 0 {
		return errors.New("geocoder returned no coordinates")
	}

	result, err := db.ExecContext(ctx, `
		UPDATE locations
		SET latitude = ?,
			longitude = ?,
			coverage_geometry = ST_GeomFromText(?, 4326),
			county_fips = CASE WHEN county_fips IS NULL OR TRIM(county_fips) = '' THEN ? ELSE county_fips END,
			nws_zone = CASE WHEN nws_zone IS NULL OR TRIM(nws_zone) = '' THEN ? ELSE nws_zone END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND coverage_geometry IS NULL`,
		resolved.Latitude,
		resolved.Longitude,
		geocode.PointWKT(resolved.Latitude, resolved.Longitude),
		nullableString(resolved.CountyFIPS),
		nullableString(resolved.NWSZone),
		locationID,
	)
	if err != nil {
		return fmt.Errorf("update location: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read location update result: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("location is no longer eligible for backfill")
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func summarize(rows []reportRow) (matched, updated, failed int) {
	for _, row := range rows {
		switch row.Status {
		case "matched":
			matched++
		case "updated":
			updated++
		case "failed":
			failed++
		}
	}
	return matched, updated, failed
}

func writeReport(path string, result report) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

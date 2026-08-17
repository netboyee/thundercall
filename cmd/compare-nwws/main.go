package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"thundercall-go/internal/models"
	"thundercall-go/internal/nwwscompare"
)

const timeFormat = "2006-01-02 15:04:05 MST"

type options struct {
	goDSN        string
	legacyDSN    string
	since        string
	until        string
	window       time.Duration
	limit        int
	strict       bool
	queryTimeout time.Duration
}

func main() {
	opts := parseFlags()

	since, until, err := resolveWindow(opts)
	if err != nil {
		fatalf("resolve window: %v", err)
	}

	goDB, err := sql.Open("mysql", opts.goDSN)
	if err != nil {
		fatalf("open Go MySQL: %v", err)
	}
	defer goDB.Close()
	if err := ensureConnection(goDB, opts.queryTimeout); err != nil {
		fatalf("connect Go MySQL: %v", err)
	}

	legacyDB, err := sql.Open("postgres", opts.legacyDSN)
	if err != nil {
		fatalf("open legacy Postgres: %v", err)
	}
	defer legacyDB.Close()
	if err := ensureConnection(legacyDB, opts.queryTimeout); err != nil {
		fatalf("connect legacy Postgres: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.queryTimeout)
	defer cancel()

	goMessages, err := fetchGoMessages(ctx, goDB, since, until, opts.limit)
	if err != nil {
		fatalf("query Go messages: %v", err)
	}

	legacyMessages, err := fetchLegacyMessages(ctx, legacyDB, since, until, opts.limit)
	if err != nil {
		fatalf("query legacy messages: %v", err)
	}

	report := nwwscompare.BuildReport(since, until, goMessages, legacyMessages)
	printReport(report)

	if opts.strict && (len(report.OnlyInGo) > 0 || len(report.OnlyInLegacy) > 0) {
		os.Exit(1)
	}
}

func parseFlags() options {
	defaultGoDSN := envOrDefault("THUNDERCALL_MYSQL_DSN", "thundercall:thundercall@tcp(127.0.0.1:3306)/thundercall?charset=utf8mb4&parseTime=true&loc=UTC&timeout=5s&readTimeout=15s&writeTimeout=15s")
	defaultLegacyDSN := envOrDefault("THUNDERCALL_LEGACY_PG_DSN", buildLegacyDSN())

	opts := options{}
	flag.StringVar(&opts.goDSN, "go-dsn", defaultGoDSN, "Go MySQL DSN")
	flag.StringVar(&opts.legacyDSN, "legacy-dsn", defaultLegacyDSN, "legacy Postgres DSN")
	flag.StringVar(&opts.since, "since", "", "comparison window start (RFC3339)")
	flag.StringVar(&opts.until, "until", "", "comparison window end (RFC3339)")
	flag.DurationVar(&opts.window, "window", 30*time.Minute, "window size used when -since is omitted")
	flag.IntVar(&opts.limit, "limit", 500, "maximum rows to fetch from each system")
	flag.BoolVar(&opts.strict, "strict", false, "exit non-zero when mismatches are found")
	flag.DurationVar(&opts.queryTimeout, "timeout", 15*time.Second, "database query timeout")
	flag.Parse()

	return opts
}

func resolveWindow(opts options) (time.Time, time.Time, error) {
	until := time.Now().UTC()
	if strings.TrimSpace(opts.until) != "" {
		parsed, err := time.Parse(time.RFC3339, opts.until)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse -until: %w", err)
		}
		until = parsed.UTC()
	}

	if strings.TrimSpace(opts.since) == "" {
		return until.Add(-opts.window), until, nil
	}

	since, err := time.Parse(time.RFC3339, opts.since)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse -since: %w", err)
	}

	return since.UTC(), until, nil
}

func buildLegacyDSN() string {
	host := envOrDefault("THUNDERCALL_LEGACY_PG_HOST", "10.0.1.199")
	port := envOrDefault("THUNDERCALL_LEGACY_PG_PORT", "5432")
	database := envOrDefault("THUNDERCALL_LEGACY_PG_DATABASE", "thundercall")
	user := envOrDefault("THUNDERCALL_LEGACY_PG_USER", "postgres")
	password := envOrDefault("THUNDERCALL_LEGACY_PG_PASSWORD", "postgres")
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=prefer&connect_timeout=5", user, password, host, port, database)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func ensureConnection(db *sql.DB, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return db.PingContext(ctx)
}

func fetchGoMessages(ctx context.Context, db *sql.DB, since, until time.Time, limit int) ([]nwwscompare.Message, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			m.id,
			COALESCE(s.awips_id, ''),
			m.event_code,
			m.message_type,
			COALESCE(m.coordinate, ''),
			COALESCE(m.polygon_wkt, ''),
			m.fips_codes_json,
			m.nws_zones_json,
			m.body,
			COALESCE(m.original_payload, ''),
			m.received_at
		FROM messages m
		LEFT JOIN source_messages s ON s.id = m.source_message_id
		WHERE m.source = 'NWWS'
		  AND m.received_at >= ?
		  AND m.received_at < ?
		ORDER BY m.received_at ASC, m.id ASC
		LIMIT ?`,
		since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []nwwscompare.Message
	for rows.Next() {
		var (
			id          int64
			awipsID     string
			eventCode   string
			messageType string
			coordinate  string
			polygon     string
			fips        models.StringSlice
			zones       models.StringSlice
			body        string
			original    string
			receivedAt  time.Time
		)

		if err := rows.Scan(&id, &awipsID, &eventCode, &messageType, &coordinate, &polygon, &fips, &zones, &body, &original, &receivedAt); err != nil {
			return nil, err
		}

		messages = append(messages, nwwscompare.Message{
			System:      "go",
			ID:          fmt.Sprintf("%d", id),
			EventCode:   eventCode,
			AWIPSID:     awipsID,
			MessageType: messageType,
			Coordinate:  coordinate,
			PolygonWKT:  polygon,
			FIPSCodes:   []string(fips),
			NWSZones:    []string(zones),
			Body:        body,
			Original:    original,
			ReceivedAt:  receivedAt.UTC(),
		})
	}

	return messages, rows.Err()
}

func fetchLegacyMessages(ctx context.Context, db *sql.DB, since, until time.Time, limit int) ([]nwwscompare.Message, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			"Id",
			COALESCE("Title", ''),
			COALESCE("Body", ''),
			COALESCE("Coordinate", ''),
			COALESCE("Polygon", ''),
			COALESCE("FipsCodes", ''),
			COALESCE("NwsZones", ''),
			COALESCE("Original", ''),
			"Timestamp"
		FROM "PendingMessages"
		WHERE "Title" LIKE '%National Weather Wire Service Message%'
		  AND "Timestamp" >= $1
		  AND "Timestamp" < $2
		ORDER BY "Timestamp" ASC, "Id" ASC
		LIMIT $3`,
		since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []nwwscompare.Message
	for rows.Next() {
		var (
			id         string
			title      string
			body       string
			coordinate string
			polygon    string
			fips       string
			zones      string
			original   string
			receivedAt time.Time
		)

		if err := rows.Scan(&id, &title, &body, &coordinate, &polygon, &fips, &zones, &original, &receivedAt); err != nil {
			return nil, err
		}

		messages = append(messages, nwwscompare.Message{
			System:      "legacy",
			ID:          id,
			MessageType: title,
			Coordinate:  coordinate,
			PolygonWKT:  polygon,
			FIPSCodes:   []string{fips},
			NWSZones:    []string{zones},
			Body:        body,
			Original:    original,
			ReceivedAt:  receivedAt.UTC(),
		})
	}

	return messages, rows.Err()
}

func printReport(report nwwscompare.Report) {
	fmt.Printf("Compared NWWS messages from %s to %s\n", report.Since.Format(time.RFC3339), report.Until.Format(time.RFC3339))
	fmt.Printf("Go rows: %d\n", report.GoCount)
	fmt.Printf("Legacy rows: %d\n", report.LegacyCount)
	fmt.Printf("Matched: %d\n", report.MatchedCount)
	fmt.Printf("Only in Go: %d\n", len(report.OnlyInGo))
	fmt.Printf("Only in legacy: %d\n", len(report.OnlyInLegacy))

	if len(report.OnlyInGo) > 0 {
		fmt.Println()
		fmt.Println("Only in Go:")
		for _, msg := range report.OnlyInGo {
			fmt.Printf("  %s | id=%s | event=%s | awips=%s | vtec=%s | fips=%s | zones=%s | polygon=%s\n",
				msg.ReceivedAt.Format(timeFormat),
				msg.ID,
				blankAs(msg.EventCode, "?"),
				blankAs(msg.AWIPSID, "?"),
				blankAs(msg.VTEC, "?"),
				summarizeList(msg.FIPSCodes),
				summarizeList(msg.NWSZones),
				summarize(msg.PolygonWKT, 72),
			)
		}
	}

	if len(report.OnlyInLegacy) > 0 {
		fmt.Println()
		fmt.Println("Only in legacy:")
		for _, msg := range report.OnlyInLegacy {
			fmt.Printf("  %s | id=%s | event=%s | awips=%s | vtec=%s | fips=%s | zones=%s | polygon=%s\n",
				msg.ReceivedAt.Format(timeFormat),
				msg.ID,
				blankAs(msg.EventCode, "?"),
				blankAs(msg.AWIPSID, "?"),
				blankAs(msg.VTEC, "?"),
				summarizeList(msg.FIPSCodes),
				summarizeList(msg.NWSZones),
				summarize(msg.PolygonWKT, 72),
			)
		}
	}
}

func summarizeList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return summarize(strings.Join(values, ","), 72)
}

func summarize(value string, max int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if len(value) <= max {
		return value
	}
	return value[:max-3] + "..."
}

func blankAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

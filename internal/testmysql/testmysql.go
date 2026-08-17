package testmysql

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

const dsnEnvVar = "THUNDERCALL_TEST_MYSQL_DSN"

type Harness struct {
	DB     *sql.DB
	DBName string
}

func Open(t testing.TB) *Harness {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(dsnEnvVar))
	if dsn == "" {
		t.Skipf("%s is not set", dsnEnvVar)
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", dsnEnvVar, err)
	}
	if !strings.Contains(strings.ToLower(cfg.DBName), "test") {
		t.Fatalf("%s must target a disposable test database, got DB name %q", dsnEnvVar, cfg.DBName)
	}

	adminDB := openDB(t, adminDSN(cfg))
	baseName := sanitizeDBName(cfg.DBName)
	testDBName := fmt.Sprintf("%s_%d_%04d", baseName, time.Now().UTC().UnixNano(), rand.New(rand.NewSource(time.Now().UTC().UnixNano())).Intn(10000))

	execContext(t, adminDB, "CREATE DATABASE `"+testDBName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")

	testCfg := *cfg
	testCfg.DBName = testDBName
	db := openDB(t, testCfg.FormatDSN())

	applySchema(t, db)

	t.Cleanup(func() {
		_ = db.Close()
		execBestEffort(adminDB, "DROP DATABASE IF EXISTS `"+testDBName+"`")
		_ = adminDB.Close()
	})

	return &Harness{
		DB:     db,
		DBName: testDBName,
	}
}

func openDB(t testing.TB, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping mysql: %v", err)
	}

	return db
}

func adminDSN(cfg *mysql.Config) string {
	adminCfg := *cfg
	adminCfg.DBName = ""
	return adminCfg.FormatDSN()
}

func applySchema(t testing.TB, db *sql.DB) {
	t.Helper()

	schemaPath := filepath.Join(repoRoot(t), "db", "schema.sql")
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema file: %v", err)
	}

	for _, statement := range splitSQLStatements(string(content)) {
		normalized := strings.TrimSpace(statement)
		lower := strings.ToLower(normalized)
		if normalized == "" || strings.HasPrefix(lower, "create database ") || strings.HasPrefix(lower, "use ") {
			continue
		}
		execContext(t, db, normalized)
	}
}

func repoRoot(t testing.TB) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve runtime caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func splitSQLStatements(content string) []string {
	var (
		statements []string
		builder    strings.Builder
	)

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, builder.String())
			builder.Reset()
		}
	}

	if tail := strings.TrimSpace(builder.String()); tail != "" {
		statements = append(statements, tail)
	}

	return statements
}

func execContext(t testing.TB, db *sql.DB, statement string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, statement); err != nil {
		t.Fatalf("exec SQL statement failed: %v\nstatement:\n%s", err, statement)
	}
}

func execBestEffort(db *sql.DB, statement string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = db.ExecContext(ctx, statement)
}

func sanitizeDBName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}

	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return "thundercall_test"
	}
	return result
}

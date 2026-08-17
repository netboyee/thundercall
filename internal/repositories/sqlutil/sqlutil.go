package sqlutil

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func Placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func Int64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func StringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func Float64Ptr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	v := value.Float64
	return &v
}

func IntPtr[T ~int](value sql.NullInt64) *T {
	if !value.Valid {
		return nil
	}
	v := T(value.Int64)
	return &v
}

func TimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}

func Int64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func StringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func Float64Value(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func IntValue[T ~int](value *T) any {
	if value == nil {
		return nil
	}
	return int64(*value)
}

func TimeValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func IsDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

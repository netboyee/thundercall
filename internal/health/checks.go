package health

import (
	"context"
	"fmt"
	"time"

	"thundercall-go/internal/config"
	"thundercall-go/internal/database"
	"thundercall-go/internal/queue/redisstreams"
)

func CheckMySQL(ctx context.Context, cfg config.MySQLConfig) error {
	if !cfg.Enabled() {
		return fmt.Errorf("THUNDERCALL_MYSQL_DSN is required")
	}

	db, err := database.OpenMySQL(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()

	return nil
}

func CheckRedis(ctx context.Context, cfg config.RedisConfig) error {
	if !cfg.Enabled() {
		return fmt.Errorf("redis configuration is required")
	}

	queue := redisstreams.New(cfg)
	defer queue.Close()

	if err := queue.Ping(ctx); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}

func CheckHeartbeat(cfg config.HealthConfig) error {
	if cfg.HeartbeatPath == "" {
		return nil
	}
	return VerifyHeartbeatFile(cfg.HeartbeatPath, cfg.HeartbeatMaxAge, time.Now().UTC())
}

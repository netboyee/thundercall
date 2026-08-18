package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"thundercall-go/internal/config"
	"thundercall-go/internal/database"
	"thundercall-go/internal/health"
	"thundercall-go/internal/ingest"
	"thundercall-go/internal/nwws"
	"thundercall-go/internal/queue/redisstreams"
	outboxeventsrepo "thundercall-go/internal/repositories/outboxevents"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch currentCommand() {
	case "healthcheck":
		if err := runHealthcheck(cfg); err != nil {
			log.Fatalf("ingest healthcheck: %v", err)
		}
	default:
		if err := run(cfg); err != nil {
			log.Fatalf("run ingest: %v", err)
		}
	}
}

func currentCommand() string {
	if len(os.Args) >= 2 && os.Args[1] == "healthcheck" {
		return "healthcheck"
	}
	return "serve"
}

func run(cfg config.Config) error {
	if !cfg.MySQL.Enabled() {
		return fmt.Errorf("THUNDERCALL_MYSQL_DSN is required for ingest")
	}
	if !cfg.Redis.Enabled() {
		return fmt.Errorf("redis configuration is required for ingest")
	}
	if !cfg.NWWS.Enabled() {
		return fmt.Errorf("NWWS credentials are required for ingest")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()

	queue := redisstreams.New(cfg.Redis)
	defer queue.Close()

	if err := queue.Ping(ctx); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}

	heartbeat := health.NewFileHeartbeat(cfg.Health.HeartbeatPath)
	touchHeartbeat := func() {
		if err := heartbeat.Touch(); err != nil {
			log.Printf("update ingest heartbeat: %v", err)
		}
	}
	touchHeartbeat()

	service := ingest.NewService(db, cfg.Redis.StreamKey, cfg.NWWS.Products)
	relay := ingest.NewOutboxRelay(outboxeventsrepo.New(db), queue, cfg.Ingest.PublishBatchSize)
	consumer := ingest.NewNWWSConsumer(cfg.NWWS, func(ctx context.Context, envelope nwws.StanzaEnvelope) error {
		result, err := service.ProcessEnvelope(ctx, envelope)
		if err == nil {
			log.Printf(
				"processed NWWS envelope id=%s awips=%s accepted=%d ignored=%d duplicate=%t source_message_id=%d message_ids=%v",
				envelope.ExternalID,
				envelope.AWIPSID,
				result.AcceptedCount,
				result.IgnoredCount,
				result.Duplicate,
				result.SourceMessageID,
				result.MessageIDs,
			)
		}
		return err
	})
	consumer.SetHeartbeatTouch(touchHeartbeat)

	relayErr := make(chan error, 1)
	go func() {
		relayErr <- relay.Run(ctx, cfg.Ingest.PublishInterval)
	}()

	log.Printf("thundercall ingest is connecting to NWWS room %s", cfg.NWWS.RoomJID())
	if err := consumer.RunForever(ctx, 10*time.Second); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run NWWS consumer: %w", err)
	}

	if err := <-relayErr; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run outbox relay: %w", err)
	}
	return nil
}

func runHealthcheck(cfg config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := health.CheckMySQL(ctx, cfg.MySQL); err != nil {
		return err
	}
	if err := health.CheckRedis(ctx, cfg.Redis); err != nil {
		return err
	}
	if err := health.CheckHeartbeat(cfg.Health); err != nil {
		return err
	}
	return nil
}

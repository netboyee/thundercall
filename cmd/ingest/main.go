package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"thundercall-go/internal/config"
	"thundercall-go/internal/database"
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
	if !cfg.MySQL.Enabled() {
		log.Fatal("THUNDERCALL_MYSQL_DSN is required for ingest")
	}
	if !cfg.Redis.Enabled() {
		log.Fatal("redis configuration is required for ingest")
	}
	if !cfg.NWWS.Enabled() {
		log.Fatal("NWWS credentials are required for ingest")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer db.Close()

	queue := redisstreams.New(cfg.Redis)
	defer queue.Close()

	if err := queue.Ping(ctx); err != nil {
		log.Fatalf("ping redis: %v", err)
	}

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

	relayErr := make(chan error, 1)
	go func() {
		relayErr <- relay.Run(ctx, cfg.Ingest.PublishInterval)
	}()

	log.Printf("thundercall ingest is connecting to NWWS room %s", cfg.NWWS.RoomJID())
	if err := consumer.RunForever(ctx, 10*time.Second); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run NWWS consumer: %v", err)
	}

	if err := <-relayErr; err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run outbox relay: %v", err)
	}
}

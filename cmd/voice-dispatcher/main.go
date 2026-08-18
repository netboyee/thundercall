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
	"thundercall-go/internal/logging"
	twilioprovider "thundercall-go/internal/providers/twilio"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	notificationsrepo "thundercall-go/internal/repositories/notifications"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
	"thundercall-go/internal/voicedispatcher"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	logging.Configure(cfg.LogLevel)

	switch currentCommand() {
	case "healthcheck":
		if err := runHealthcheck(cfg); err != nil {
			log.Fatalf("voice-dispatcher healthcheck: %v", err)
		}
	default:
		if err := run(cfg); err != nil {
			log.Fatalf("run voice-dispatcher: %v", err)
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
	logger := logging.New("voice-dispatcher")
	if !cfg.MySQL.Enabled() {
		return fmt.Errorf("THUNDERCALL_MYSQL_DSN is required for voice-dispatcher")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()

	heartbeat := health.NewFileHeartbeat(cfg.Health.HeartbeatPath)
	touchHeartbeat := func() {
		if err := heartbeat.Touch(); err != nil {
			logger.Warnf("event=heartbeat_update_error error=%q", err)
		}
	}
	touchHeartbeat()

	service := voicedispatcher.NewService(
		deliveryattemptsrepo.New(db),
		usersmessagesrepo.New(db),
		notificationsrepo.New(db),
		twilioprovider.New(cfg.Twilio),
		voicedispatcher.NewPacer(cfg.Voice.CallsPerSecond),
		cfg.Voice.RetryDelay,
	)
	service.SetCallsPerSecond(cfg.Voice.CallsPerSecond)
	runner := voicedispatcher.NewRunner(
		deliveryattemptsrepo.New(db),
		service,
		cfg.Voice.ConsumerName,
		cfg.Voice.ClaimBatchSize,
		cfg.Voice.ClaimLease,
		cfg.Voice.IdleSleep,
	)
	runner.SetHeartbeatTouch(touchHeartbeat)

	logger.Infof(
		"event=start consumer=%s cps=%d batch=%d",
		cfg.Voice.ConsumerName,
		cfg.Voice.CallsPerSecond,
		cfg.Voice.ClaimBatchSize,
	)
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run voice-dispatcher: %w", err)
	}
	return nil
}

func runHealthcheck(cfg config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := health.CheckMySQL(ctx, cfg.MySQL); err != nil {
		return err
	}
	if err := health.CheckHeartbeat(cfg.Health); err != nil {
		return err
	}
	return nil
}

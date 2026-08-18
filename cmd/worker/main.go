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
	sendgridprovider "thundercall-go/internal/providers/sendgrid"
	twilioprovider "thundercall-go/internal/providers/twilio"
	"thundercall-go/internal/queue/redisstreams"
	accountsettingsrepo "thundercall-go/internal/repositories/accountsettings"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	locationsrepo "thundercall-go/internal/repositories/locations"
	messagesrepo "thundercall-go/internal/repositories/messages"
	notificationsrepo "thundercall-go/internal/repositories/notifications"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersettingsrepo "thundercall-go/internal/repositories/usersettings"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
	"thundercall-go/internal/thundercall"
	"thundercall-go/internal/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch currentCommand() {
	case "healthcheck":
		if err := runHealthcheck(cfg); err != nil {
			log.Fatalf("worker healthcheck: %v", err)
		}
	default:
		if err := run(cfg); err != nil {
			log.Fatalf("run worker: %v", err)
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
		return fmt.Errorf("THUNDERCALL_MYSQL_DSN is required for worker")
	}
	if !cfg.Redis.Enabled() {
		return fmt.Errorf("redis configuration is required for worker")
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
			log.Printf("update worker heartbeat: %v", err)
		}
	}
	touchHeartbeat()

	resolver := thundercall.NewSQLRecipientResolver(
		locationsrepo.New(db),
		userlocationsrepo.New(db),
		accountsettingsrepo.New(db),
		usersettingsrepo.New(db),
	)
	dispatcher := thundercall.NewChannelDispatcher(
		usercontactmethodsrepo.New(db),
		usersmessagesrepo.New(db),
		deliveryattemptsrepo.New(db),
		notificationsrepo.New(db),
		twilioprovider.New(cfg.Twilio),
		sendgridprovider.New(cfg.SendGrid),
	)
	service := worker.NewService(messagesrepo.New(db), resolver, dispatcher)
	runner := worker.NewRunner(queue, service, cfg.Worker.ReadCount, cfg.Redis.Block, 5*time.Second)
	runner.SetHeartbeatTouch(touchHeartbeat)

	log.Printf("thundercall worker is consuming redis stream %s as group %s consumer %s", cfg.Redis.StreamKey, cfg.Redis.ConsumerGroup, cfg.Redis.ConsumerName)
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run worker: %w", err)
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

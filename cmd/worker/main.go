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
	if !cfg.MySQL.Enabled() {
		log.Fatal("THUNDERCALL_MYSQL_DSN is required for worker")
	}
	if !cfg.Redis.Enabled() {
		log.Fatal("redis configuration is required for worker")
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

	log.Printf("thundercall worker is consuming redis stream %s as group %s consumer %s", cfg.Redis.StreamKey, cfg.Redis.ConsumerGroup, cfg.Redis.ConsumerName)
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run worker: %v", err)
	}
}

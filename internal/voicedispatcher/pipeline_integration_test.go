//go:build integration

package voicedispatcher

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"thundercall-go/internal/config"
	"thundercall-go/internal/ingest"
	"thundercall-go/internal/models"
	"thundercall-go/internal/nwws"
	twilioprovider "thundercall-go/internal/providers/twilio"
	"thundercall-go/internal/queue/redisstreams"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	accountsettingsrepo "thundercall-go/internal/repositories/accountsettings"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	locationsrepo "thundercall-go/internal/repositories/locations"
	messagesrepo "thundercall-go/internal/repositories/messages"
	notificationsrepo "thundercall-go/internal/repositories/notifications"
	outboxeventsrepo "thundercall-go/internal/repositories/outboxevents"
	sourcemessagesrepo "thundercall-go/internal/repositories/sourcemessages"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersrepo "thundercall-go/internal/repositories/users"
	usersettingsrepo "thundercall-go/internal/repositories/usersettings"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
	"thundercall-go/internal/testmysql"
	"thundercall-go/internal/thundercall"
	"thundercall-go/internal/worker"
)

func TestIntegrationIngestOutboxWorkerVoiceDispatcherPipeline(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	queue, streamKey, redisCleanup := openRedisStreamHarness(t, "pipeline")
	defer redisCleanup()
	defer func() { _ = queue.Close() }()

	accountRepo := accountsrepo.New(harness.DB)
	userRepo := usersrepo.New(harness.DB)
	locationRepo := locationsrepo.New(harness.DB)
	userLocationRepo := userlocationsrepo.New(harness.DB)
	contactRepo := usercontactmethodsrepo.New(harness.DB)
	accountSettingsRepo := accountsettingsrepo.New(harness.DB)
	userSettingsRepo := usersettingsrepo.New(harness.DB)
	sourceRepo := sourcemessagesrepo.New(harness.DB)
	messageRepo := messagesrepo.New(harness.DB)
	outboxRepo := outboxeventsrepo.New(harness.DB)
	userMessageRepo := usersmessagesrepo.New(harness.DB)
	attemptRepo := deliveryattemptsrepo.New(harness.DB)
	notificationRepo := notificationsrepo.New(harness.DB)

	account := &models.Account{Name: "Pipeline Integration", Active: true}
	if err := accountRepo.Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	insideUser := createVoiceTestUser(t, ctx, userRepo, account.ID, "Inside User")
	createVoiceContact(t, ctx, contactRepo, insideUser.ID, "+15551110001")
	insideLocation := createVoiceLocation(
		t,
		ctx,
		locationRepo,
		account.ID,
		"Inside Polygon",
		"POLYGON ((41.53 -94.21,41.64 -93.82,41.37 -93.69,41.26 -94.06,41.53 -94.21))",
	)
	createVoiceSubscription(t, ctx, userLocationRepo, insideUser.ID, insideLocation.ID)

	outsideUser := createVoiceTestUser(t, ctx, userRepo, account.ID, "Outside User")
	createVoiceContact(t, ctx, contactRepo, outsideUser.ID, "+15551110002")
	outsideLocation := createVoiceLocation(
		t,
		ctx,
		locationRepo,
		account.ID,
		"Outside Polygon",
		"POLYGON ((35.00 -90.00,35.00 -89.50,35.50 -89.50,35.50 -90.00,35.00 -90.00))",
	)
	createVoiceSubscription(t, ctx, userLocationRepo, outsideUser.ID, outsideLocation.ID)

	resolver := thundercall.NewSQLRecipientResolver(locationRepo, userLocationRepo, accountSettingsRepo, userSettingsRepo)
	dispatcher := thundercall.NewChannelDispatcher(contactRepo, userMessageRepo, attemptRepo, notificationRepo)
	workerService := worker.NewService(messageRepo, resolver, dispatcher)
	workerRunner := worker.NewRunner(queue, workerService, 10, 25*time.Millisecond, 25*time.Millisecond)

	sender := &recordingVoiceSender{}
	dispatchService := NewService(attemptRepo, userMessageRepo, notificationRepo, sender, &fakeWaiter{}, 30*time.Second)
	dispatchService.logf = func(string, ...any) {}
	voiceRunner := NewRunner(attemptRepo, dispatchService, "voice-pipeline", 10, time.Minute, 25*time.Millisecond)
	voiceRunner.logf = func(string, ...any) {}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerErrs := make(chan error, 1)
	voiceErrs := make(chan error, 1)
	go func() { workerErrs <- workerRunner.Run(runCtx) }()
	go func() { voiceErrs <- voiceRunner.Run(runCtx) }()

	body := mustReadSample(t, "examples_01_SVRDMX.txt")
	externalID := fmt.Sprintf("pipeline-%d", time.Now().UTC().UnixNano())
	ingestService := ingest.NewService(harness.DB, streamKey, []string{"SVR", "FFW", "TOR", "WSW", "TSU"})
	result, err := ingestService.ProcessEnvelope(ctx, nwws.StanzaEnvelope{
		CCCCode:    "KDMX",
		WMOCode:    "WUUS53",
		IssueTime:  time.Date(2026, 8, 15, 21, 18, 0, 0, time.UTC),
		AWIPSID:    "SVRDMX",
		ExternalID: externalID,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("ProcessEnvelope() error = %v", err)
	}
	if result.AcceptedCount != 1 {
		t.Fatalf("AcceptedCount = %d, want 1", result.AcceptedCount)
	}
	if len(result.MessageIDs) != 1 {
		t.Fatalf("len(MessageIDs) = %d, want 1", len(result.MessageIDs))
	}

	relay := ingest.NewOutboxRelay(outboxRepo, queue, 10)
	publishResult, err := relay.PublishOnce(ctx)
	if err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}
	if publishResult.Published != 1 {
		t.Fatalf("Published = %d, want 1", publishResult.Published)
	}
	if publishResult.Failed != 0 {
		t.Fatalf("Failed = %d, want 0", publishResult.Failed)
	}

	messageID := result.MessageIDs[0]
	waitForCondition(t, 10*time.Second, 25*time.Millisecond, func() (bool, string) {
		message, err := messageRepo.GetByID(ctx, messageID)
		if err != nil {
			return false, fmt.Sprintf("GetByID(message=%d): %v", messageID, err)
		}
		if message == nil {
			return false, "message not found yet"
		}
		if message.Status != "processed" {
			return false, "message not processed yet"
		}

		userMessages, err := userMessageRepo.ListByMessageID(ctx, messageID)
		if err != nil {
			return false, fmt.Sprintf("ListByMessageID(message=%d): %v", messageID, err)
		}
		if len(userMessages) != 1 {
			return false, fmt.Sprintf("user_messages=%d, want 1 matched user", len(userMessages))
		}
		if userMessages[0].UserID != insideUser.ID {
			return false, fmt.Sprintf("matched user_id=%d, want %d", userMessages[0].UserID, insideUser.ID)
		}
		if userMessages[0].Status != "sent" {
			return false, fmt.Sprintf("user_message status=%q, want sent", userMessages[0].Status)
		}

		attempt, err := attemptRepo.GetByUserMessageIDAndChannel(ctx, userMessages[0].ID, models.ChannelVoice)
		if err != nil {
			return false, fmt.Sprintf("GetByUserMessageIDAndChannel(user_message=%d): %v", userMessages[0].ID, err)
		}
		if attempt == nil {
			return false, "voice delivery attempt not created yet"
		}
		if attempt.Status != "sent" {
			return false, fmt.Sprintf("delivery_attempt status=%q, want sent", attempt.Status)
		}

		if message.NWSEventID == nil {
			return false, "message missing nws_event_id"
		}
		notification, err := notificationRepo.GetByEventUserChannel(ctx, *message.NWSEventID, insideUser.ID, models.ChannelVoice)
		if err != nil {
			return false, fmt.Sprintf("GetByEventUserChannel(event=%d user=%d): %v", *message.NWSEventID, insideUser.ID, err)
		}
		if notification == nil {
			return false, "notification missing"
		}
		if notification.Status != "sent" {
			return false, fmt.Sprintf("notification status=%q, want sent", notification.Status)
		}

		if got := sender.CallCount(); got != 1 {
			return false, fmt.Sprintf("sender call count=%d, want 1", got)
		}

		publishedCount, err := publishedOutboxCount(ctx, harness.DB, messageID)
		if err != nil {
			return false, fmt.Sprintf("publishedOutboxCount(message=%d): %v", messageID, err)
		}
		if publishedCount != 1 {
			return false, fmt.Sprintf("published outbox rows=%d, want 1", publishedCount)
		}

		return true, ""
	})

	sourceMessage, err := sourceRepo.GetBySourceAndExternalID(ctx, "NWWS", externalID)
	if err != nil {
		t.Fatalf("GetBySourceAndExternalID(%q) error = %v", externalID, err)
	}
	if sourceMessage == nil {
		t.Fatalf("source_message missing for external_id=%q", externalID)
	}
	if sourceMessage.Status != "parsed" {
		t.Fatalf("source_message status = %q, want parsed", sourceMessage.Status)
	}

	calls := sender.Snapshot()
	if len(calls) != 1 {
		t.Fatalf("len(sender calls) = %d, want 1", len(calls))
	}
	if calls[0].Request.To != "+15551110001" {
		t.Fatalf("call destination = %q, want inside user phone", calls[0].Request.To)
	}
	if strings.TrimSpace(calls[0].Request.EventCode) != "SVR" {
		t.Fatalf("call event code = %q, want SVR", calls[0].Request.EventCode)
	}

	cancel()
	assertRunnerStopped(t, <-workerErrs)
	assertRunnerStopped(t, <-voiceErrs)
}

func TestIntegrationVoiceDispatcherRespectsCPSAndFairnessAcrossMessages(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	accountRepo := accountsrepo.New(harness.DB)
	userRepo := usersrepo.New(harness.DB)
	messageRepo := messagesrepo.New(harness.DB)
	userMessageRepo := usersmessagesrepo.New(harness.DB)
	attemptRepo := deliveryattemptsrepo.New(harness.DB)

	account := &models.Account{Name: "Voice CPS Integration", Active: true}
	if err := accountRepo.Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	messageA := createDispatchMessage(t, ctx, messageRepo, account.ID, "msg-a", "SVR", "body-a", time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC))
	messageB := createDispatchMessage(t, ctx, messageRepo, account.ID, "msg-b", "TOR", "body-b", time.Date(2026, 8, 18, 15, 0, 1, 0, time.UTC))
	messageC := createDispatchMessage(t, ctx, messageRepo, account.ID, "msg-c", "FFW", "body-c", time.Date(2026, 8, 18, 15, 0, 2, 0, time.UTC))

	var expectedBodies []string
	requestedBase := time.Now().UTC().Add(-2 * time.Minute)
	for round := 0; round < 4; round++ {
		for idx, message := range []*models.Message{messageA, messageB, messageC} {
			user := createVoiceTestUser(t, ctx, userRepo, account.ID, fmt.Sprintf("CPS User %d-%d", idx+1, round+1))
			destination := fmt.Sprintf("+1555222%04d", round*10+idx+1)
			createQueuedVoiceAttempt(t, ctx, userMessageRepo, attemptRepo, message.ID, user.ID, destination, requestedBase.Add(time.Duration(round*3+idx)*time.Second))
			expectedBodies = append(expectedBodies, message.Body)
		}
	}

	sender := &recordingVoiceSender{}
	service := NewService(attemptRepo, userMessageRepo, nil, sender, NewPacer(4), 30*time.Second)
	service.logf = func(string, ...any) {}

	runner := NewRunner(attemptRepo, service, "voice-cps", 12, time.Minute, 20*time.Millisecond)
	runner.logf = func(string, ...any) {}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- runner.Run(runCtx) }()

	waitForCondition(t, 10*time.Second, 20*time.Millisecond, func() (bool, string) {
		if sender.CallCount() != len(expectedBodies) {
			return false, fmt.Sprintf("sender call count=%d, want %d", sender.CallCount(), len(expectedBodies))
		}
		return true, ""
	})

	cancel()
	assertRunnerStopped(t, <-errCh)

	calls := sender.Snapshot()
	if len(calls) != len(expectedBodies) {
		t.Fatalf("len(calls) = %d, want %d", len(calls), len(expectedBodies))
	}

	gotBodies := make([]string, 0, len(calls))
	for _, call := range calls {
		gotBodies = append(gotBodies, call.Request.Body)
	}
	if len(gotBodies) != len(expectedBodies) {
		t.Fatalf("len(gotBodies) = %d, want %d", len(gotBodies), len(expectedBodies))
	}
	for i := range expectedBodies {
		if gotBodies[i] != expectedBodies[i] {
			t.Fatalf("call body order[%d] = %q, want %q (full order=%v)", i, gotBodies[i], expectedBodies[i], gotBodies)
		}
	}

	if len(calls) >= 2 {
		elapsed := calls[len(calls)-1].At.Sub(calls[0].At)
		minElapsed := time.Duration(float64((len(calls)-1)*250) * 0.85 * float64(time.Millisecond))
		if elapsed < minElapsed {
			t.Fatalf("dispatch elapsed = %s, want at least %s to respect 4 CPS pacing", elapsed, minElapsed)
		}
	}

	var sentAttempts int
	if err := harness.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_attempts WHERE status = 'sent'`).Scan(&sentAttempts); err != nil {
		t.Fatalf("count sent delivery attempts: %v", err)
	}
	if sentAttempts != len(expectedBodies) {
		t.Fatalf("sent delivery attempts = %d, want %d", sentAttempts, len(expectedBodies))
	}
}

type recordingVoiceSender struct {
	mu           sync.Mutex
	calls        []observedVoiceCall
	nextProvider int
}

type observedVoiceCall struct {
	Request twilioprovider.VoiceRequest
	At      time.Time
}

func (s *recordingVoiceSender) SendVoice(_ context.Context, request twilioprovider.VoiceRequest) (twilioprovider.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, observedVoiceCall{
		Request: request,
		At:      time.Now().UTC(),
	})
	s.nextProvider++
	return twilioprovider.Result{
		Provider:          "twilio_voice",
		ProviderMessageID: fmt.Sprintf("recorded-%d", s.nextProvider),
		Status:            "queued",
	}, nil
}

func (s *recordingVoiceSender) ResolveVoiceDestination(to string) (string, bool) {
	return to, false
}

func (s *recordingVoiceSender) BuildTestVoiceBody(_ string, body string) string {
	return body
}

func (s *recordingVoiceSender) BuildCollapsedTestVoiceBody(_ string, body string) string {
	return body
}

func (s *recordingVoiceSender) CollapseVoiceOverrideCalls() bool {
	return false
}

func (s *recordingVoiceSender) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *recordingVoiceSender) Snapshot() []observedVoiceCall {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]observedVoiceCall, len(s.calls))
	copy(out, s.calls)
	return out
}

func openRedisStreamHarness(t *testing.T, name string) (*redisstreams.Client, string, func()) {
	t.Helper()

	suffix := fmt.Sprintf("%s-%d", sanitizeRedisName(name), time.Now().UTC().UnixNano())
	addr := firstNonEmptyString(
		os.Getenv("THUNDERCALL_TEST_REDIS_ADDR"),
		os.Getenv("THUNDERCALL_REDIS_ADDR"),
		"127.0.0.1:6379",
	)
	password := firstNonEmptyString(
		os.Getenv("THUNDERCALL_TEST_REDIS_PASSWORD"),
		os.Getenv("THUNDERCALL_REDIS_PASSWORD"),
	)

	cfg := config.RedisConfig{
		Addr:          addr,
		Password:      password,
		DB:            0,
		StreamKey:     "thundercall:test:" + suffix,
		ConsumerGroup: "group-" + suffix,
		ConsumerName:  "worker-" + suffix,
		ClaimMinIdle:  10 * time.Millisecond,
	}

	raw := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := raw.Ping(ctx).Err(); err != nil {
		_ = raw.Close()
		t.Skipf("redis unavailable at %s: %v", cfg.Addr, err)
	}

	queue := redisstreams.New(cfg)
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()
		_ = raw.Del(cleanupCtx, cfg.StreamKey).Err()
		_ = raw.Close()
	}
	return queue, cfg.StreamKey, cleanup
}

func mustReadSample(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "nwws", "testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", name, err)
	}
	return string(body)
}

func waitForCondition(t *testing.T, timeout time.Duration, interval time.Duration, check func() (bool, string)) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastReason string
	for time.Now().Before(deadline) {
		ok, reason := check()
		if ok {
			return
		}
		lastReason = reason
		time.Sleep(interval)
	}

	t.Fatalf("condition not met within %s: %s", timeout, lastReason)
}

func publishedOutboxCount(ctx context.Context, db *sql.DB, aggregateID int64) (int, error) {
	var count int
	err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type = 'message' AND aggregate_id = ? AND published_at IS NOT NULL`,
		aggregateID,
	).Scan(&count)
	return count, err
}

func createDispatchMessage(t *testing.T, ctx context.Context, repo *messagesrepo.Repository, accountID int64, fingerprint string, eventCode string, body string, receivedAt time.Time) *models.Message {
	t.Helper()

	messageType := "Severe Weather Warning"
	alertType := "severe_thunderstorm_warning"
	switch eventCode {
	case "TOR":
		messageType = "Tornado Warning"
		alertType = "tornado_warning"
	case "FFW":
		messageType = "Flash Flood Warning"
		alertType = "flash_flood_warning"
	}

	message := &models.Message{
		AccountID:     &accountID,
		Fingerprint:   fingerprint,
		Source:        "NWWS",
		EventCode:     eventCode,
		MessageType:   messageType,
		AlertTypeCode: alertType,
		Body:          body,
		PolygonWKT:    stringPtr("POLYGON ((39.00 -85.00,39.00 -84.00,40.00 -84.00,40.00 -85.00,39.00 -85.00))"),
		Status:        "accepted",
		ReceivedAt:    receivedAt,
	}
	if err := repo.Create(ctx, message); err != nil {
		t.Fatalf("Create(message %q) error = %v", fingerprint, err)
	}
	return message
}

func sanitizeRedisName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "test"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func assertRunnerStopped(t *testing.T, err error) {
	t.Helper()
	if err == nil || err == context.Canceled {
		return
	}
	t.Fatalf("runner error = %v, want nil or context.Canceled", err)
}

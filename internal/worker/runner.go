package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"thundercall-go/internal/events"
	"thundercall-go/internal/queue/redisstreams"
)

type queueClient interface {
	EnsureGroup(ctx context.Context) error
	AutoClaim(ctx context.Context, start string, count int64) ([]redisstreams.StreamMessage, string, error)
	ReadGroup(ctx context.Context, count int64, block time.Duration) ([]redisstreams.StreamMessage, error)
	Ack(ctx context.Context, ids ...string) error
}

type messageProcessor interface {
	ProcessMessage(ctx context.Context, messageID int64) error
}

type Runner struct {
	queue      queueClient
	service    messageProcessor
	readCount  int64
	block      time.Duration
	retryDelay time.Duration
	logf       func(string, ...any)
}

func NewRunner(queue queueClient, service messageProcessor, readCount int64, block time.Duration, retryDelay time.Duration) *Runner {
	if readCount <= 0 {
		readCount = 25
	}
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}

	return &Runner{
		queue:      queue,
		service:    service,
		readCount:  readCount,
		block:      block,
		retryDelay: retryDelay,
		logf:       log.Printf,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if r.queue == nil {
		return fmt.Errorf("redis queue client is required")
	}
	if r.service == nil {
		return fmt.Errorf("worker service is required")
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := r.queue.EnsureGroup(ctx); err != nil {
			r.logf("worker ensure redis stream consumer group: %v", err)
			if err := r.waitRetry(ctx); err != nil {
				return err
			}
			continue
		}

		claimed, _, err := r.queue.AutoClaim(ctx, "0-0", r.readCount)
		if err != nil {
			r.logf("worker auto-claim redis stream messages: %v", err)
			if err := r.waitRetry(ctx); err != nil {
				return err
			}
			continue
		}
		r.processMessages(ctx, claimed)

		messages, err := r.queue.ReadGroup(ctx, r.readCount, r.block)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.logf("worker read redis stream consumer group: %v", err)
			if err := r.waitRetry(ctx); err != nil {
				return err
			}
			continue
		}
		r.processMessages(ctx, messages)
	}
}

func (r *Runner) processMessages(ctx context.Context, messages []redisstreams.StreamMessage) {
	for _, message := range messages {
		if ctx.Err() != nil {
			return
		}

		switch message.EventType {
		case events.EventTypeMessageAccepted:
			event, err := events.DecodeMessageAccepted(message.Payload)
			if err != nil {
				r.logf("worker dropping malformed stream message %s: %v", message.ID, err)
				r.ackBestEffort(ctx, message.ID, "malformed stream message")
				continue
			}

			if err := r.service.ProcessMessage(ctx, event.MessageID); err != nil {
				if errors.Is(err, ErrMessageNotFound) {
					r.logf("worker dropping stream message %s for missing message %d", message.ID, event.MessageID)
					r.ackBestEffort(ctx, message.ID, "missing message")
					continue
				}
				r.logf("worker leaving stream message %s pending for retry after processing error: %v", message.ID, err)
				continue
			}
			r.ackBestEffort(ctx, message.ID, "processed message")
		default:
			r.logf("worker dropping unsupported stream event %q (%s)", message.EventType, message.ID)
			r.ackBestEffort(ctx, message.ID, "unsupported stream event")
		}
	}
}

func (r *Runner) ackBestEffort(ctx context.Context, id string, reason string) {
	if err := r.queue.Ack(ctx, id); err != nil {
		r.logf("worker failed to ack stream message %s after %s: %v", id, reason, err)
	}
}

func (r *Runner) waitRetry(ctx context.Context) error {
	timer := time.NewTimer(r.retryDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

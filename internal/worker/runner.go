package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"thundercall-go/internal/events"
	"thundercall-go/internal/logging"
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
	debugf     func(string, ...any)
	warnf      func(string, ...any)
	touch      func()
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
		debugf:     logging.New("worker.runner").Debugf,
		warnf:      logging.New("worker.runner").Warnf,
	}
}

func (r *Runner) SetHeartbeatTouch(fn func()) {
	r.touch = fn
}

func (r *Runner) Run(ctx context.Context) error {
	if r.queue == nil {
		return fmt.Errorf("redis queue client is required")
	}
	if r.service == nil {
		return fmt.Errorf("worker service is required")
	}

	for {
		r.markHealthy()

		if err := ctx.Err(); err != nil {
			return err
		}

		if err := r.queue.EnsureGroup(ctx); err != nil {
			r.warnf("event=worker_ensure_group_failed error=%q", err)
			if err := r.waitRetry(ctx); err != nil {
				return err
			}
			continue
		}
		r.markHealthy()

		claimed, _, err := r.queue.AutoClaim(ctx, "0-0", r.readCount)
		if err != nil {
			r.warnf("event=worker_autoclaim_failed error=%q", err)
			if err := r.waitRetry(ctx); err != nil {
				return err
			}
			continue
		}
		r.markHealthy()
		r.processMessages(ctx, claimed)

		messages, err := r.queue.ReadGroup(ctx, r.readCount, r.block)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.warnf("event=worker_read_group_failed error=%q", err)
			if err := r.waitRetry(ctx); err != nil {
				return err
			}
			continue
		}
		r.markHealthy()
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
				r.warnf("event=worker_drop_malformed stream_id=%s error=%q", message.ID, err)
				r.ackBestEffort(ctx, message.ID, "malformed stream message")
				continue
			}

			if err := r.service.ProcessMessage(ctx, event.MessageID); err != nil {
				if errors.Is(err, ErrMessageNotFound) {
					r.warnf("event=worker_drop_missing stream_id=%s message_id=%d", message.ID, event.MessageID)
					r.ackBestEffort(ctx, message.ID, "missing message")
					continue
				}
				r.warnf("event=worker_process_retry stream_id=%s message_id=%d error=%q", message.ID, event.MessageID, err)
				continue
			}
			r.debugf("event=worker_processed stream_id=%s message_id=%d", message.ID, event.MessageID)
			r.markHealthy()
			r.ackBestEffort(ctx, message.ID, "processed message")
		default:
			r.warnf("event=worker_drop_unsupported stream_id=%s event_type=%s", message.ID, message.EventType)
			r.ackBestEffort(ctx, message.ID, "unsupported stream event")
		}
	}
}

func (r *Runner) markHealthy() {
	if r.touch != nil {
		r.touch()
	}
}

func (r *Runner) ackBestEffort(ctx context.Context, id string, reason string) {
	if err := r.queue.Ack(ctx, id); err != nil {
		r.warnf("event=worker_ack_failed stream_id=%s reason=%q error=%q", id, reason, err)
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

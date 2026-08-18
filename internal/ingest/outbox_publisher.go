package ingest

import (
	"context"
	"fmt"
	"time"

	"thundercall-go/internal/logging"
	"thundercall-go/internal/models"
)

type outboxRepository interface {
	ListUnpublished(ctx context.Context, limit int) ([]models.OutboxEvent, error)
	MarkPublished(ctx context.Context, id int64, publishedAt time.Time) error
	MarkFailed(ctx context.Context, id int64, lastError string) error
}

type streamPublisher interface {
	Publish(ctx context.Context, streamKey string, eventType string, aggregateType string, aggregateID int64, payload string) (string, error)
}

type PublishResult struct {
	Published int
	Failed    int
}

type OutboxRelay struct {
	outbox    outboxRepository
	publisher streamPublisher
	batchSize int
	now       func() time.Time
	debugf    func(string, ...any)
	warnf     func(string, ...any)
}

func NewOutboxRelay(outbox outboxRepository, publisher streamPublisher, batchSize int) *OutboxRelay {
	if batchSize <= 0 {
		batchSize = 50
	}

	return &OutboxRelay{
		outbox:    outbox,
		publisher: publisher,
		batchSize: batchSize,
		now:       func() time.Time { return time.Now().UTC() },
		debugf:    logging.New("ingest.outbox").Debugf,
		warnf:     logging.New("ingest.outbox").Warnf,
	}
}

func (r *OutboxRelay) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		result, err := r.PublishOnce(ctx)
		if err != nil && ctx.Err() == nil {
			r.warnf("event=outbox_publish_batch_error error=%q", err)
		}
		if result.Failed > 0 {
			r.warnf(
				"event=outbox_publish_batch_result published=%d failed=%d",
				result.Published,
				result.Failed,
			)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *OutboxRelay) PublishOnce(ctx context.Context) (PublishResult, error) {
	if r.outbox == nil {
		return PublishResult{}, fmt.Errorf("outbox repository is required")
	}
	if r.publisher == nil {
		return PublishResult{}, fmt.Errorf("redis publisher is required")
	}

	events, err := r.outbox.ListUnpublished(ctx, r.batchSize)
	if err != nil {
		return PublishResult{}, fmt.Errorf("list unpublished outbox events: %w", err)
	}

	result := PublishResult{}
	for _, event := range events {
		if _, err := r.publisher.Publish(ctx, event.StreamKey, event.EventType, event.AggregateType, event.AggregateID, event.PayloadJSON); err != nil {
			result.Failed++
			r.warnf(
				"event=outbox_publish_failed outbox_event_id=%d stream=%s aggregate_type=%s aggregate_id=%d error=%q",
				event.ID,
				event.StreamKey,
				event.AggregateType,
				event.AggregateID,
				err,
			)
			if markErr := r.outbox.MarkFailed(ctx, event.ID, err.Error()); markErr != nil {
				return result, fmt.Errorf("publish outbox event %d failed with %q and mark failed also failed: %w", event.ID, err.Error(), markErr)
			}
			continue
		}

		if err := r.outbox.MarkPublished(ctx, event.ID, r.now()); err != nil {
			result.Failed++
			lastError := "published to stream but failed to mark published: " + err.Error()
			r.warnf(
				"event=outbox_mark_published_failed outbox_event_id=%d stream=%s aggregate_type=%s aggregate_id=%d error=%q",
				event.ID,
				event.StreamKey,
				event.AggregateType,
				event.AggregateID,
				err,
			)
			if markErr := r.outbox.MarkFailed(ctx, event.ID, lastError); markErr != nil {
				return result, fmt.Errorf("mark outbox event %d published: %w (mark failed also failed: %v)", event.ID, err, markErr)
			}
			continue
		}
		r.debugf(
			"event=outbox_published outbox_event_id=%d stream=%s aggregate_type=%s aggregate_id=%d event_type=%s",
			event.ID,
			event.StreamKey,
			event.AggregateType,
			event.AggregateID,
			event.EventType,
		)
		result.Published++
	}

	return result, nil
}

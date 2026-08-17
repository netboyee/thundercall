package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"thundercall-go/internal/models"
)

func TestOutboxRelayPublishOnceContinuesAfterPublishFailure(t *testing.T) {
	t.Parallel()

	repo := &fakeOutboxRepository{
		events: []models.OutboxEvent{
			{ID: 1, AggregateID: 101, AggregateType: "message", EventType: "message.accepted", StreamKey: "stream", PayloadJSON: `{"messageId":101}`},
			{ID: 2, AggregateID: 102, AggregateType: "message", EventType: "message.accepted", StreamKey: "stream", PayloadJSON: `{"messageId":102}`},
		},
	}
	publisher := &fakeStreamPublisher{
		errs: map[int64]error{
			101: errors.New("redis unavailable"),
		},
	}

	relay := NewOutboxRelay(repo, publisher, 10)
	relay.logf = func(string, ...any) {}
	relay.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }

	result, err := relay.PublishOnce(context.Background())
	if err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}
	if result.Published != 1 || result.Failed != 1 {
		t.Fatalf("PublishOnce() result = %+v, want 1 published and 1 failed", result)
	}
	if got, want := repo.failedIDs, []int64{1}; !equalInt64s(got, want) {
		t.Fatalf("failed IDs = %v, want %v", got, want)
	}
	if got, want := repo.publishedIDs, []int64{2}; !equalInt64s(got, want) {
		t.Fatalf("published IDs = %v, want %v", got, want)
	}
}

func TestOutboxRelayPublishOnceMarksFailedWhenPublishSucceedsButPersistFails(t *testing.T) {
	t.Parallel()

	repo := &fakeOutboxRepository{
		events: []models.OutboxEvent{
			{ID: 1, AggregateID: 201, AggregateType: "message", EventType: "message.accepted", StreamKey: "stream", PayloadJSON: `{"messageId":201}`},
		},
		publishErrs: map[int64]error{
			1: errors.New("update outbox row failed"),
		},
	}
	publisher := &fakeStreamPublisher{}

	relay := NewOutboxRelay(repo, publisher, 10)
	relay.logf = func(string, ...any) {}

	result, err := relay.PublishOnce(context.Background())
	if err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}
	if result.Published != 0 || result.Failed != 1 {
		t.Fatalf("PublishOnce() result = %+v, want 0 published and 1 failed", result)
	}
	if got, want := repo.failedIDs, []int64{1}; !equalInt64s(got, want) {
		t.Fatalf("failed IDs = %v, want %v", got, want)
	}
}

func equalInt64s(got []int64, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type fakeOutboxRepository struct {
	events       []models.OutboxEvent
	publishedIDs []int64
	failedIDs    []int64
	publishErrs  map[int64]error
	failMessages map[int64]string
}

func (r *fakeOutboxRepository) ListUnpublished(context.Context, int) ([]models.OutboxEvent, error) {
	return append([]models.OutboxEvent(nil), r.events...), nil
}

func (r *fakeOutboxRepository) MarkPublished(_ context.Context, id int64, _ time.Time) error {
	if err := r.publishErrs[id]; err != nil {
		return err
	}
	r.publishedIDs = append(r.publishedIDs, id)
	return nil
}

func (r *fakeOutboxRepository) MarkFailed(_ context.Context, id int64, lastError string) error {
	r.failedIDs = append(r.failedIDs, id)
	if r.failMessages == nil {
		r.failMessages = make(map[int64]string)
	}
	r.failMessages[id] = lastError
	return nil
}

type fakeStreamPublisher struct {
	errs map[int64]error
}

func (p *fakeStreamPublisher) Publish(_ context.Context, _ string, _ string, _ string, aggregateID int64, _ string) (string, error) {
	if p.errs != nil {
		if err := p.errs[aggregateID]; err != nil {
			return "", err
		}
	}
	return "1-0", nil
}

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"thundercall-go/internal/events"
	"thundercall-go/internal/queue/redisstreams"
)

func TestRunnerProcessMessagesDropsMalformedAndUnsupportedEvents(t *testing.T) {
	t.Parallel()

	queue := &fakeQueue{}
	processor := &fakeProcessor{}
	runner := NewRunner(queue, processor, 25, 0, 0)
	runner.logf = func(string, ...any) {}

	validPayload, err := events.EncodeMessageAccepted(42)
	if err != nil {
		t.Fatalf("EncodeMessageAccepted() error = %v", err)
	}

	runner.processMessages(context.Background(), []redisstreams.StreamMessage{
		{ID: "1-0", EventType: events.EventTypeMessageAccepted, Payload: "{not-json"},
		{ID: "2-0", EventType: events.EventTypeMessageAccepted, Payload: validPayload},
		{ID: "3-0", EventType: "unsupported.event", Payload: `{"messageId":99}`},
	})

	if len(processor.processed) != 1 || processor.processed[0] != 42 {
		t.Fatalf("processed message IDs = %v, want [42]", processor.processed)
	}
	if got, want := queue.acked, []string{"1-0", "2-0", "3-0"}; !equalStrings(got, want) {
		t.Fatalf("acked IDs = %v, want %v", got, want)
	}
}

func TestRunnerProcessMessagesLeavesTransientFailuresPending(t *testing.T) {
	t.Parallel()

	queue := &fakeQueue{}
	processor := &fakeProcessor{
		errs: map[int64]error{
			100: errors.New("twilio timeout"),
			101: ErrMessageNotFound,
		},
	}
	runner := NewRunner(queue, processor, 25, 0, 0)
	runner.logf = func(string, ...any) {}

	payload100, err := events.EncodeMessageAccepted(100)
	if err != nil {
		t.Fatalf("EncodeMessageAccepted(100) error = %v", err)
	}
	payload101, err := events.EncodeMessageAccepted(101)
	if err != nil {
		t.Fatalf("EncodeMessageAccepted(101) error = %v", err)
	}
	payload102, err := events.EncodeMessageAccepted(102)
	if err != nil {
		t.Fatalf("EncodeMessageAccepted(102) error = %v", err)
	}

	runner.processMessages(context.Background(), []redisstreams.StreamMessage{
		{ID: "10-0", EventType: events.EventTypeMessageAccepted, Payload: payload100},
		{ID: "11-0", EventType: events.EventTypeMessageAccepted, Payload: payload101},
		{ID: "12-0", EventType: events.EventTypeMessageAccepted, Payload: payload102},
	})

	if got, want := processor.processed, []int64{100, 101, 102}; !equalInt64s(got, want) {
		t.Fatalf("processed message IDs = %v, want %v", got, want)
	}
	if got, want := queue.acked, []string{"11-0", "12-0"}; !equalStrings(got, want) {
		t.Fatalf("acked IDs = %v, want %v", got, want)
	}
}

type fakeQueue struct {
	acked []string
}

func (*fakeQueue) EnsureGroup(context.Context) error {
	return nil
}

func (*fakeQueue) AutoClaim(context.Context, string, int64) ([]redisstreams.StreamMessage, string, error) {
	return nil, "0-0", nil
}

func (*fakeQueue) ReadGroup(context.Context, int64, time.Duration) ([]redisstreams.StreamMessage, error) {
	return nil, nil
}

func (q *fakeQueue) Ack(_ context.Context, ids ...string) error {
	q.acked = append(q.acked, ids...)
	return nil
}

type fakeProcessor struct {
	processed []int64
	errs      map[int64]error
}

func (p *fakeProcessor) ProcessMessage(_ context.Context, messageID int64) error {
	p.processed = append(p.processed, messageID)
	if p.errs == nil {
		return nil
	}
	return p.errs[messageID]
}

func equalStrings(got []string, want []string) bool {
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

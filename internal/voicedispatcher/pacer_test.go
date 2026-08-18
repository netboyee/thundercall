package voicedispatcher

import (
	"context"
	"testing"
	"time"
)

func TestPacerSpacesCallsByConfiguredCPS(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	var sleeps []time.Duration

	pacer := NewPacer(2)
	pacer.now = func() time.Time { return current }
	pacer.sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		current = current.Add(delay)
		return nil
	}

	if err := pacer.Wait(context.Background()); err != nil {
		t.Fatalf("Wait(first) error = %v", err)
	}
	if err := pacer.Wait(context.Background()); err != nil {
		t.Fatalf("Wait(second) error = %v", err)
	}
	if err := pacer.Wait(context.Background()); err != nil {
		t.Fatalf("Wait(third) error = %v", err)
	}

	if got := len(sleeps); got != 2 {
		t.Fatalf("sleep count = %d, want 2", got)
	}
	if sleeps[0] != 500*time.Millisecond {
		t.Fatalf("first paced delay = %s, want 500ms", sleeps[0])
	}
	if sleeps[1] != 500*time.Millisecond {
		t.Fatalf("second paced delay = %s, want 500ms", sleeps[1])
	}
}

package voicedispatcher

import (
	"context"
	"sync"
	"time"
)

type turnWaiter interface {
	Wait(ctx context.Context) error
}

type Pacer struct {
	interval time.Duration
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error

	mu   sync.Mutex
	next time.Time
}

func NewPacer(callsPerSecond int) *Pacer {
	if callsPerSecond <= 0 {
		callsPerSecond = 1
	}

	return &Pacer{
		interval: time.Second / time.Duration(callsPerSecond),
		now:      func() time.Time { return time.Now().UTC() },
		sleep:    sleepWithContext,
	}
}

func (p *Pacer) Wait(ctx context.Context) error {
	if p == nil || p.interval <= 0 {
		return nil
	}

	p.mu.Lock()
	current := p.now()
	delay := time.Duration(0)
	if !p.next.IsZero() && current.Before(p.next) {
		delay = p.next.Sub(current)
		p.next = p.next.Add(p.interval)
	} else {
		p.next = current.Add(p.interval)
	}
	p.mu.Unlock()

	if delay <= 0 {
		return nil
	}
	return p.sleep(ctx, delay)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

package voicedispatcher

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"thundercall-go/internal/logging"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
)

type claimsRepository interface {
	ClaimQueuedVoiceAttempts(ctx context.Context, leaseToken string, leaseOwner string, now time.Time, leaseDuration time.Duration, limit int) ([]deliveryattemptsrepo.VoiceDispatchRecord, error)
}

type attemptProcessor interface {
	ProcessAttempt(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord) error
}

type Runner struct {
	attempts    claimsRepository
	processor   attemptProcessor
	consumer    string
	claimBatch  int
	claimLease  time.Duration
	idleSleep   time.Duration
	now         func() time.Time
	infof       func(string, ...any)
	warnf       func(string, ...any)
	touch       func()
	leaseSerial uint64
}

func NewRunner(attempts claimsRepository, processor attemptProcessor, consumer string, claimBatch int, claimLease time.Duration, idleSleep time.Duration) *Runner {
	if claimBatch <= 0 {
		claimBatch = 25
	}
	if claimLease <= 0 {
		claimLease = 2 * time.Minute
	}
	if idleSleep <= 0 {
		idleSleep = 2 * time.Second
	}

	logger := logging.New("voice-dispatcher.runner")
	return &Runner{
		attempts:   attempts,
		processor:  processor,
		consumer:   consumer,
		claimBatch: claimBatch,
		claimLease: claimLease,
		idleSleep:  idleSleep,
		now:        func() time.Time { return time.Now().UTC() },
		infof:      logger.Infof,
		warnf:      logger.Warnf,
	}
}

func (r *Runner) SetHeartbeatTouch(fn func()) {
	r.touch = fn
}

func (r *Runner) Run(ctx context.Context) error {
	if r.attempts == nil {
		return fmt.Errorf("delivery attempts repository is required")
	}
	if r.processor == nil {
		return fmt.Errorf("voice attempt processor is required")
	}

	for {
		r.markHealthy()
		if err := ctx.Err(); err != nil {
			return err
		}

		records, err := r.attempts.ClaimQueuedVoiceAttempts(
			ctx,
			r.nextLeaseToken(),
			r.consumer,
			r.now(),
			r.claimLease,
			r.claimBatch,
		)
		if err != nil {
			r.warnf("event=voice_claim_error consumer=%s error=%q", r.consumer, err)
			if err := r.wait(ctx, r.idleSleep); err != nil {
				return err
			}
			continue
		}

		if len(records) == 0 {
			if err := r.wait(ctx, r.idleSleep); err != nil {
				return err
			}
			continue
		}
		if r.infof != nil {
			r.infof("event=voice_claim_batch consumer=%s claimed=%d", r.consumer, len(records))
		}

		for _, record := range records {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.processor.ProcessAttempt(ctx, record); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				r.warnf(
					"event=voice_process_error attempt_id=%d message_id=%d user_id=%d error=%q",
					record.Attempt.ID,
					record.MessageID,
					record.UserID,
					err,
				)
			}
			r.markHealthy()
		}
	}
}

func (r *Runner) markHealthy() {
	if r.touch != nil {
		r.touch()
	}
}

func (r *Runner) wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Runner) nextLeaseToken() string {
	serial := atomic.AddUint64(&r.leaseSerial, 1)
	return fmt.Sprintf("%s-%d-%d", r.consumer, r.now().UnixNano(), serial)
}

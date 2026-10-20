//go:build solution

package retry

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

func Do(ctx context.Context, p Policy, fn func(context.Context) error) error {
	sleep := p.Sleep
	if sleep == nil {
		sleep = realSleep
	}
	var err error
	for attempt := range max(p.Attempts, 1) {
		if attempt > 0 {
			if serr := sleep(ctx, backoff(p, attempt)); serr != nil {
				return serr
			}
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		err = fn(ctx)
		if err == nil {
			return nil
		}
		if perm, ok := errors.AsType[*permanentError](err); ok {
			return perm.err
		}
	}
	return err
}

// backoff — full jitter: rand[0, min(max, base·2^(n-1))].
func backoff(p Policy, attempt int) time.Duration {
	d := p.BaseDelay << (attempt - 1)
	if d <= 0 || (p.MaxDelay > 0 && d > p.MaxDelay) { // d <= 0 — переполнение сдвига
		d = p.MaxDelay
	}
	if d <= 0 {
		return 0
	}
	return rand.N(d + 1)
}

func realSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

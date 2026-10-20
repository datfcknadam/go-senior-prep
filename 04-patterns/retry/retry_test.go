package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

var errTemp = errors.New("temporary")

func recordSleeps(delays *[]time.Duration) func(context.Context, time.Duration) error {
	return func(ctx context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return ctx.Err()
	}
}

func TestSuccessAfterRetries(t *testing.T) {
	var delays []time.Duration
	calls := 0
	err := Do(context.Background(), Policy{Attempts: 5, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second, Sleep: recordSleeps(&delays)},
		func(context.Context) error {
			calls++
			if calls < 3 {
				return errTemp
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(delays) != 2 {
		t.Fatalf("calls=%d sleeps=%d, ожидалось 3 и 2", calls, len(delays))
	}
}

func TestExhausted(t *testing.T) {
	var delays []time.Duration
	calls := 0
	err := Do(context.Background(), Policy{Attempts: 4, BaseDelay: 100 * time.Millisecond, MaxDelay: 250 * time.Millisecond, Sleep: recordSleeps(&delays)},
		func(context.Context) error {
			calls++
			return fmt.Errorf("attempt %d: %w", calls, errTemp)
		})
	if calls != 4 {
		t.Fatalf("calls = %d, want 4", calls)
	}
	if err == nil || err.Error() != "attempt 4: temporary" {
		t.Fatalf("нужно вернуть последнюю ошибку, got %v", err)
	}
	caps := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 250 * time.Millisecond}
	for i, d := range delays {
		if d < 0 || d > caps[i] {
			t.Errorf("задержка %d = %v вне [0, %v]", i+1, d, caps[i])
		}
	}
}

func TestJitterIsRandom(t *testing.T) {
	seen := map[time.Duration]bool{}
	for range 20 {
		var delays []time.Duration
		Do(context.Background(), Policy{Attempts: 2, BaseDelay: time.Second, MaxDelay: time.Second, Sleep: recordSleeps(&delays)},
			func(context.Context) error { return errTemp })
		seen[delays[0]] = true
	}
	if len(seen) < 5 {
		t.Fatal("задержки не случайны — нет jitter")
	}
}

func TestPermanent(t *testing.T) {
	notFound := errors.New("404")
	calls := 0
	err := Do(context.Background(), Policy{Attempts: 5, BaseDelay: time.Millisecond, Sleep: recordSleeps(new([]time.Duration))},
		func(context.Context) error {
			calls++
			return Permanent(notFound)
		})
	if calls != 1 {
		t.Fatalf("неповторяемую ошибку ретраили %d раз", calls)
	}
	if err != notFound {
		t.Fatalf("нужно вернуть исходную ошибку без обёртки, got %v", err)
	}
	if Permanent(nil) != nil {
		t.Error("Permanent(nil) должен вернуть nil")
	}
}

func TestContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	calls := 0
	start := time.Now()
	err := Do(ctx, Policy{Attempts: 10, BaseDelay: time.Second, MaxDelay: time.Second},
		func(context.Context) error {
			calls++
			return errTemp
		})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("ожидание не прерывается отменой контекста")
	}
	if calls > 2 {
		t.Fatalf("после отмены продолжились вызовы: %d", calls)
	}
}

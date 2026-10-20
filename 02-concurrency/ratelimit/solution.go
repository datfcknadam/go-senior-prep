//go:build solution

package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	mu     sync.Mutex
	rate   float64 // токенов в секунду
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

func New(rate float64, burst int) *Limiter {
	return NewWithClock(rate, burst, time.Now)
}

func NewWithClock(rate float64, burst int, now func() time.Time) *Limiter {
	return &Limiter{
		rate:   rate,
		burst:  float64(burst),
		tokens: float64(burst),
		last:   now(),
		now:    now,
	}
}

// refill досыпает токены за прошедшее время. Вызывать под мьютексом.
func (l *Limiter) refill() {
	t := l.now()
	if elapsed := t.Sub(l.last).Seconds(); elapsed > 0 {
		l.tokens = min(l.burst, l.tokens+elapsed*l.rate)
	}
	l.last = t
}

func (l *Limiter) Allow() bool {
	_, ok := l.take()
	return ok
}

// take забирает токен; если его нет — возвращает, сколько ждать до следующего.
func (l *Limiter) take() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	if l.tokens >= 1 {
		l.tokens--
		return 0, true
	}
	need := (1 - l.tokens) / l.rate
	return time.Duration(need * float64(time.Second)), false
}

func (l *Limiter) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		wait, ok := l.take()
		if ok {
			return nil
		}
		// Ждём вне мьютекса. После таймера пробуем снова: токен мог забрать кто-то другой.
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

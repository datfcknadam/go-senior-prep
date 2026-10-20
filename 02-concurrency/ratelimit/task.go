//go:build !solution

package ratelimit

import (
	"context"
	"time"
)

type Limiter struct {
	// TODO
}

func New(rate float64, burst int) *Limiter {
	return NewWithClock(rate, burst, time.Now)
}

func NewWithClock(rate float64, burst int, now func() time.Time) *Limiter {
	panic("TODO")
}

// Allow забирает токен, если он есть.
func (l *Limiter) Allow() bool {
	panic("TODO")
}

// Wait блокируется до появления токена или отмены ctx.
func (l *Limiter) Wait(ctx context.Context) error {
	panic("TODO")
}

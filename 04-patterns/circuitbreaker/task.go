//go:build !solution

package circuitbreaker

import "time"

type Breaker struct {
	// TODO
}

func New(failureThreshold int, openTimeout time.Duration, now func() time.Time) *Breaker {
	panic("TODO")
}

func (b *Breaker) Execute(fn func() error) error {
	panic("TODO")
}

func (b *Breaker) State() State {
	panic("TODO")
}

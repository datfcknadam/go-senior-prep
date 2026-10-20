//go:build solution

package circuitbreaker

import (
	"sync"
	"time"
)

type Breaker struct {
	mu          sync.Mutex
	threshold   int
	openTimeout time.Duration
	now         func() time.Time

	state    State
	failures int
	openedAt time.Time
	probing  bool // в HalfOpen уже идёт пробный вызов
}

func New(failureThreshold int, openTimeout time.Duration, now func() time.Time) *Breaker {
	return &Breaker{threshold: failureThreshold, openTimeout: openTimeout, now: now}
}

func (b *Breaker) Execute(fn func() error) error {
	if err := b.before(); err != nil {
		return err
	}
	err := fn() // без мьютекса: медленная зависимость не должна блокировать остальных
	b.after(err)
	return err
}

func (b *Breaker) before() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.advance()
	switch b.state {
	case Open:
		return ErrOpen
	case HalfOpen:
		if b.probing {
			return ErrOpen
		}
		b.probing = true
	}
	return nil
}

func (b *Breaker) after(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case HalfOpen:
		b.probing = false
		if err != nil {
			b.trip()
		} else {
			b.state, b.failures = Closed, 0
		}
	case Closed:
		if err == nil {
			b.failures = 0
			return
		}
		b.failures++
		if b.failures >= b.threshold {
			b.trip()
		}
	}
}

func (b *Breaker) trip() {
	b.state = Open
	b.openedAt = b.now()
	b.failures = 0
}

// advance лениво переводит Open → HalfOpen по таймауту. Вызывать под мьютексом.
func (b *Breaker) advance() {
	if b.state == Open && b.now().Sub(b.openedAt) >= b.openTimeout {
		b.state = HalfOpen
		b.probing = false
	}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.advance()
	return b.state
}

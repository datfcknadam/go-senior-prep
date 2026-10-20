package circuitbreaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var errDep = errors.New("dependency failed")

func fail() error { return errDep }
func ok() error   { return nil }

func TestOpensAfterConsecutiveFailures(t *testing.T) {
	clk := &clock{t: time.Unix(0, 0)}
	b := New(3, time.Second, clk.Now)

	b.Execute(fail)
	b.Execute(fail)
	b.Execute(ok) // сбрасывает счётчик
	b.Execute(fail)
	b.Execute(fail)
	if b.State() != Closed {
		t.Fatal("ошибки не подряд — должен остаться Closed")
	}
	if err := b.Execute(fail); !errors.Is(err, errDep) {
		t.Fatalf("Execute должен вернуть ошибку fn, got %v", err)
	}
	if b.State() != Open {
		t.Fatal("3 ошибки подряд → Open")
	}

	called := false
	err := b.Execute(func() error { called = true; return nil })
	if !errors.Is(err, ErrOpen) || called {
		t.Fatal("в Open fn не вызывается, возвращается ErrOpen")
	}
}

func TestHalfOpenRecovery(t *testing.T) {
	clk := &clock{t: time.Unix(0, 0)}
	b := New(1, time.Second, clk.Now)
	b.Execute(fail)

	clk.Advance(999 * time.Millisecond)
	if b.State() != Open {
		t.Fatal("таймаут ещё не прошёл")
	}
	clk.Advance(time.Millisecond)
	if b.State() != HalfOpen {
		t.Fatal("после openTimeout → HalfOpen")
	}

	b.Execute(fail)
	if b.State() != Open {
		t.Fatal("ошибка пробного вызова → Open")
	}

	clk.Advance(time.Second)
	if err := b.Execute(ok); err != nil {
		t.Fatal(err)
	}
	if b.State() != Closed {
		t.Fatal("успешный пробный вызов → Closed")
	}
}

func TestHalfOpenSingleProbe(t *testing.T) {
	clk := &clock{t: time.Unix(0, 0)}
	b := New(1, time.Second, clk.Now)
	b.Execute(fail)
	clk.Advance(time.Second)

	release := make(chan struct{})
	var calls, rejected atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			err := b.Execute(func() error {
				calls.Add(1)
				<-release
				return nil
			})
			if errors.Is(err, ErrOpen) {
				rejected.Add(1)
			}
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if calls.Load() != 1 || rejected.Load() != 19 {
		t.Fatalf("в HalfOpen прошло %d вызовов, отклонено %d; ожидалось 1 и 19", calls.Load(), rejected.Load())
	}
}

func TestFnRunsWithoutLock(t *testing.T) {
	b := New(5, time.Second, time.Now)
	started := make(chan struct{})
	release := make(chan struct{})
	go b.Execute(func() error { close(started); <-release; return nil })
	<-started

	done := make(chan struct{})
	go func() { b.Execute(ok); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("медленный вызов блокирует остальные: fn выполняется под мьютексом?")
	}
	close(release)
}

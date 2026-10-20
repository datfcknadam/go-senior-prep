package ratelimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestAllowBurstAndRefill(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	l := NewWithClock(10, 3, clk.Now) // 10 токенов/с, ведро на 3

	for i := range 3 {
		if !l.Allow() {
			t.Fatalf("запрос %d из burst должен пройти", i)
		}
	}
	if l.Allow() {
		t.Fatal("ведро пустое, запрос должен быть отклонён")
	}

	clk.Advance(50 * time.Millisecond) // полтокена
	if l.Allow() {
		t.Fatal("накопилось только 0.5 токена")
	}
	clk.Advance(50 * time.Millisecond) // ещё полтокена → 1
	if !l.Allow() {
		t.Fatal("накопился целый токен")
	}

	clk.Advance(time.Hour)
	n := 0
	for l.Allow() {
		n++
	}
	if n != 3 {
		t.Fatalf("после долгого простоя доступно %d токенов, ожидалось burst=3", n)
	}
}

func TestAllowConcurrent(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	l := NewWithClock(1, 100, clk.Now)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			if l.Allow() {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 100 {
		t.Fatalf("прошло %d запросов, ожидалось ровно 100", ok.Load())
	}
}

func TestWait(t *testing.T) {
	l := New(100, 1) // реальное время: токен каждые 10 мс
	ctx := context.Background()
	start := time.Now()
	for range 6 {
		if err := l.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// 1 токен сразу + 5 по 10 мс ≈ 50 мс.
	if d := time.Since(start); d < 40*time.Millisecond || d > 500*time.Millisecond {
		t.Fatalf("6 токенов получены за %v, ожидалось ≈50ms", d)
	}
}

func TestWaitCancel(t *testing.T) {
	l := New(0.1, 1) // следующий токен через 10 с
	l.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := l.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("Wait не реагирует на отмену контекста")
	}
}

package lru

import (
	"fmt"
	"sync"
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

func newClock() *clock { return &clock{t: time.Unix(1_000_000, 0)} }

func TestEviction(t *testing.T) {
	c := New[string, int](2, 0, newClock().Now)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Get("a")    // a теперь свежее b
	c.Set("c", 3) // вытесняет b

	if _, ok := c.Get("b"); ok {
		t.Error("b должен быть вытеснен")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Error("a должен остаться")
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Error("c должен быть в кэше")
	}
	if c.Len() != 2 {
		t.Errorf("Len = %d", c.Len())
	}
}

func TestUpdateExisting(t *testing.T) {
	c := New[string, int](2, 0, newClock().Now)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("a", 10) // обновление делает a свежим
	c.Set("c", 3)  // вытесняет b

	if v, _ := c.Get("a"); v != 10 {
		t.Errorf("a = %d, want 10", v)
	}
	if _, ok := c.Get("b"); ok {
		t.Error("b должен быть вытеснен")
	}
	if c.Len() != 2 {
		t.Errorf("Len = %d, обновление не должно добавлять элемент", c.Len())
	}
}

func TestTTL(t *testing.T) {
	clk := newClock()
	c := New[string, int](10, time.Minute, clk.Now)
	c.Set("a", 1)

	clk.Advance(59 * time.Second)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("элемент ещё жив")
	}
	clk.Advance(time.Second)
	if _, ok := c.Get("a"); ok {
		t.Fatal("элемент протух")
	}
	if c.Len() != 0 {
		t.Error("протухший элемент нужно удалять при Get")
	}

	c.Set("b", 1)
	clk.Advance(50 * time.Second)
	c.Set("b", 2) // продлевает TTL
	clk.Advance(50 * time.Second)
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatal("Set должен обновлять TTL")
	}
}

func TestConcurrent(t *testing.T) {
	c := New[string, int](100, time.Second, newClock().Now)
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for i := range 1000 {
				k := fmt.Sprint((g*1000 + i) % 300)
				c.Set(k, i)
				c.Get(k)
			}
		})
	}
	wg.Wait()
	if c.Len() > 100 {
		t.Fatalf("Len = %d превышает capacity", c.Len())
	}
}

func BenchmarkGetSet(b *testing.B) {
	c := New[int, int](1000, 0, time.Now)
	i := 0
	for b.Loop() {
		c.Set(i%2000, i)
		c.Get((i + 1) % 2000)
		i++
	}
}

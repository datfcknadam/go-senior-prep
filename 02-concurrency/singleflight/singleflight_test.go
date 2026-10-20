package singleflight

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDedup(t *testing.T) {
	var g Group[string, int]
	var calls atomic.Int32
	release := make(chan struct{})

	const n = 100
	var wg sync.WaitGroup
	var sharedCount atomic.Int32
	results := make([]int, n)
	for i := range n {
		wg.Go(func() {
			v, err, shared := g.Do("ad:1", func() (int, error) {
				calls.Add(1)
				<-release
				return 42, nil
			})
			if err != nil {
				t.Error(err)
			}
			if shared {
				sharedCount.Add(1)
			}
			results[i] = v
		})
	}
	time.Sleep(50 * time.Millisecond) // даём всем горутинам дойти до Do
	close(release)
	wg.Wait()

	if c := calls.Load(); c != 1 {
		t.Fatalf("fn вызвана %d раз, ожидался 1", c)
	}
	for i, v := range results {
		if v != 42 {
			t.Fatalf("results[%d] = %d", i, v)
		}
	}
	if sharedCount.Load() != n {
		t.Fatalf("shared=true у %d вызовов, ожидалось у всех %d", sharedCount.Load(), n)
	}
}

func TestForgetAfterDone(t *testing.T) {
	var g Group[string, int]
	boom := errors.New("boom")
	_, err, shared := g.Do("k", func() (int, error) { return 0, boom })
	if !errors.Is(err, boom) || shared {
		t.Fatalf("got err=%v shared=%v", err, shared)
	}
	v, err, _ := g.Do("k", func() (int, error) { return 7, nil })
	if err != nil || v != 7 {
		t.Fatal("после завершения ключ должен забываться")
	}
}

func TestDifferentKeysDontBlock(t *testing.T) {
	var g Group[string, int]
	block := make(chan struct{})
	defer close(block)
	go g.Do("slow", func() (int, error) { <-block; return 0, nil })
	time.Sleep(10 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		g.Do("fast", func() (int, error) { return 1, nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("разные ключи блокируют друг друга")
	}
}

func TestPanicDoesNotHang(t *testing.T) {
	var g Group[string, int]
	func() {
		defer func() { _ = recover() }()
		g.Do("k", func() (int, error) { panic("oops") })
	}()

	done := make(chan struct{})
	go func() {
		g.Do("k", func() (int, error) { return 1, nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("после паники в fn ключ остался «занят»")
	}
}

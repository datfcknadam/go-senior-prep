package merge

import (
	"context"
	"runtime"
	"slices"
	"testing"
	"time"
)

func gen(vals ...int) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for _, v := range vals {
			ch <- v
		}
	}()
	return ch
}

func TestMergeAll(t *testing.T) {
	out := Merge(context.Background(), gen(1, 2, 3), gen(4, 5), gen())
	var got []int
	for v := range out {
		got = append(got, v)
	}
	slices.Sort(got)
	if !slices.Equal(got, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("got %v", got)
	}
}

func TestMergeNoChannels(t *testing.T) {
	select {
	case _, ok := <-Merge[int](context.Background()):
		if ok {
			t.Fatal("ожидался закрытый канал")
		}
	case <-time.After(time.Second):
		t.Fatal("канал без входов должен закрываться сразу")
	}
}

func TestMergeCancelNoLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	never := make(chan int) // никогда не закрывается
	busy := make(chan int, 1)
	busy <- 1 // значение, которое никто не прочитает из out

	out := Merge(ctx, never, busy)
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-waitClosed(out):
	case <-time.After(time.Second):
		t.Fatal("out не закрылся после отмены контекста")
	}

	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Fatalf("утечка горутин: было %d, стало %d", before, n)
	}
}

func waitClosed[T any](ch <-chan T) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	return done
}

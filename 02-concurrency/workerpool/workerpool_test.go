package workerpool

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestOrderAndResults(t *testing.T) {
	in := []int{5, 1, 4, 2, 3}
	out, err := Process(context.Background(), in, 3, func(_ context.Context, v int) (int, error) {
		time.Sleep(time.Duration(v) * time.Millisecond)
		return v * v, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out, []int{25, 1, 16, 4, 9}) {
		t.Fatalf("got %v", out)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	var cur, peak atomic.Int32
	in := make([]int, 50)
	_, err := Process(context.Background(), in, 4, func(context.Context, int) (int, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		cur.Add(-1)
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if p := peak.Load(); p > 4 {
		t.Fatalf("одновременно работало %d воркеров, лимит 4", p)
	}
	if p := peak.Load(); p < 2 {
		t.Fatalf("работа не распараллелилась: пик %d", p)
	}
}

func TestFirstErrorCancels(t *testing.T) {
	boom := errors.New("boom")
	var started, finished atomic.Int32
	in := make([]int, 100)
	for i := range in {
		in[i] = i
	}
	_, err := Process(context.Background(), in, 4, func(ctx context.Context, v int) (int, error) {
		started.Add(1)
		defer finished.Add(1)
		if v == 3 {
			return 0, boom
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Second):
			return v, nil
		}
	})
	if !errors.Is(err, boom) {
		t.Fatalf("ожидалась первая ошибка boom, получено %v", err)
	}
	if s := started.Load(); s > 10 {
		t.Errorf("после ошибки запустилось слишком много задач: %d", s)
	}
	if started.Load() != finished.Load() {
		t.Error("Process вернул управление, пока воркеры ещё работают")
	}
}

func TestParentCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Process(ctx, make([]int, 20), 2, func(ctx context.Context, _ int) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Second):
			return 0, nil
		}
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("отмена родительского контекста не остановила обработку")
	}
}

func TestEmpty(t *testing.T) {
	out, err := Process(context.Background(), []int{}, 3, func(context.Context, int) (int, error) {
		return 0, nil
	})
	if err != nil || len(out) != 0 {
		t.Fatalf("got %v, %v", out, err)
	}
}

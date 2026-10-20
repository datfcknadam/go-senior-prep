//go:build solution

package workerpool

import (
	"context"
	"sync"
)

func Process[In, Out any](
	ctx context.Context,
	inputs []In,
	workers int,
	fn func(context.Context, In) (Out, error),
) ([]Out, error) {
	if workers <= 0 {
		workers = 1
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	results := make([]Out, len(inputs))
	idx := make(chan int)

	var wg sync.WaitGroup
	for range min(workers, len(inputs)) {
		wg.Go(func() {
			for i := range idx {
				if ctx.Err() != nil {
					return // select в раздатчике мог выбрать отправку, хотя контекст уже отменён
				}
				out, err := fn(ctx, inputs[i])
				if err != nil {
					cancel(err) // первая причина отмены запоминается, остальные игнорируются
					return
				}
				// Каждая горутина пишет в свой индекс — разные ячейки памяти, гонки нет.
				results[i] = out
			}
		})
	}

	// Раздаём задачи, пока контекст жив.
feed:
	for i := range inputs {
		select {
		case idx <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(idx)
	wg.Wait()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return results, nil
}

//go:build solution

package merge

import (
	"context"
	"sync"
)

func Merge[T any](ctx context.Context, chans ...<-chan T) <-chan T {
	out := make(chan T)
	var wg sync.WaitGroup
	for _, ch := range chans {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case v, ok := <-ch:
					if !ok {
						return
					}
					// Отправка тоже под select: иначе при отмене горутина повиснет,
					// если никто не читает out.
					select {
					case out <- v:
					case <-ctx.Done():
						return
					}
				}
			}
		})
	}
	// Закрываем out ровно один раз, когда все отправители завершились.
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

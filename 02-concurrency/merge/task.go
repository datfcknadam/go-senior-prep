//go:build !solution

package merge

import "context"

// Merge объединяет каналы в один.
func Merge[T any](ctx context.Context, chans ...<-chan T) <-chan T {
	panic("TODO")
}

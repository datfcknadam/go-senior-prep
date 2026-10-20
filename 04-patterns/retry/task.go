//go:build !solution

package retry

import "context"

// Permanent помечает ошибку как неповторяемую.
func Permanent(err error) error {
	panic("TODO")
}

func Do(ctx context.Context, p Policy, fn func(context.Context) error) error {
	panic("TODO")
}

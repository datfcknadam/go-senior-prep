//go:build !solution

package workerpool

import "context"

// Process применяет fn к каждому входу не более чем в workers горутинах.
func Process[In, Out any](
	ctx context.Context,
	inputs []In,
	workers int,
	fn func(context.Context, In) (Out, error),
) ([]Out, error) {
	panic("TODO")
}

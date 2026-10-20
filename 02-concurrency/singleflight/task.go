//go:build !solution

package singleflight

type Group[K comparable, V any] struct {
	// TODO
}

func (g *Group[K, V]) Do(key K, fn func() (V, error)) (v V, err error, shared bool) {
	panic("TODO")
}

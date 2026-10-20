//go:build !solution

package lru

import "time"

type Cache[K comparable, V any] struct {
	// TODO
}

// New создаёт кэш. ttl == 0 — без срока жизни. now — источник времени.
func New[K comparable, V any](capacity int, ttl time.Duration, now func() time.Time) *Cache[K, V] {
	panic("TODO")
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	panic("TODO")
}

func (c *Cache[K, V]) Set(key K, value V) {
	panic("TODO")
}

func (c *Cache[K, V]) Len() int {
	panic("TODO")
}

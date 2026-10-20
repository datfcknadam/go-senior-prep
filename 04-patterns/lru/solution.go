//go:build solution

package lru

import (
	"container/list"
	"sync"
	"time"
)

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
}

type Cache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time
	items    map[K]*list.Element
	order    *list.List // начало — самый свежий, конец — кандидат на вытеснение
}

func New[K comparable, V any](capacity int, ttl time.Duration, now func() time.Time) *Cache[K, V] {
	if capacity <= 0 {
		panic("lru: capacity must be positive")
	}
	return &Cache[K, V]{
		capacity: capacity,
		ttl:      ttl,
		now:      now,
		items:    make(map[K]*list.Element, capacity),
		order:    list.New(),
	}
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	e := el.Value.(*entry[K, V])
	if c.expired(e) {
		c.remove(el)
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return e.value, true
}

func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var exp time.Time
	if c.ttl > 0 {
		exp = c.now().Add(c.ttl)
	}
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry[K, V])
		e.value, e.expiresAt = value, exp
		c.order.MoveToFront(el)
		return
	}
	if c.order.Len() >= c.capacity {
		c.remove(c.order.Back())
	}
	c.items[key] = c.order.PushFront(&entry[K, V]{key: key, value: value, expiresAt: exp})
}

func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

func (c *Cache[K, V]) expired(e *entry[K, V]) bool {
	return c.ttl > 0 && !c.now().Before(e.expiresAt)
}

func (c *Cache[K, V]) remove(el *list.Element) {
	c.order.Remove(el)
	delete(c.items, el.Value.(*entry[K, V]).key)
}

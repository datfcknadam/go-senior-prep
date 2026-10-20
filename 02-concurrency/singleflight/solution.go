//go:build solution

package singleflight

import "sync"

type call[V any] struct {
	wg   sync.WaitGroup
	val  V
	err  error
	dups int
}

type Group[K comparable, V any] struct {
	mu    sync.Mutex
	calls map[K]*call[V]
}

func (g *Group[K, V]) Do(key K, fn func() (V, error)) (v V, err error, shared bool) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[K]*call[V])
	}
	if c, ok := g.calls[key]; ok {
		c.dups++
		g.mu.Unlock()
		c.wg.Wait() // Wait даёт happens-before: val и err уже записаны
		return c.val, c.err, true
	}
	c := &call[V]{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	// defer: даже если fn запаникует, ждущие не повиснут навсегда, а ключ удалится.
	defer func() {
		g.mu.Lock()
		delete(g.calls, key)
		shared = c.dups > 0
		g.mu.Unlock()
		c.wg.Done()
	}()

	c.val, c.err = fn()
	return c.val, c.err, false // shared проставит defer
}

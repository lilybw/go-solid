package collections

import "sync"

type Guarded[T any] struct {
	mu    sync.RWMutex
	value T
}

func (g *Guarded[T]) Do(fn func(T)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fn(g.value)
}
func (g *Guarded[T]) Replace(value T) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = value
}

func Read[T, R any](g *Guarded[T], fn func(T) R) R {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return fn(g.value)
}
func Write[T, R any](g *Guarded[T], fn func(T) R) R {
	g.mu.Lock()
	defer g.mu.Unlock()
	return fn(g.value)
}

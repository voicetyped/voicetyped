package registry

import (
	"fmt"
	"sync"
)

// Factory creates an instance of T from a config map.
type Factory[T any] func(config map[string]string) (T, error)

// Registry holds named factories for creating instances of T.
type Registry[T any] struct {
	mu        sync.RWMutex
	factories map[string]Factory[T]
}

// New creates a new empty registry.
func New[T any]() *Registry[T] {
	return &Registry[T]{
		factories: make(map[string]Factory[T]),
	}
}

// Register adds a named factory to the registry.
func (r *Registry[T]) Register(name string, factory Factory[T]) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// Create instantiates T using the named factory.
func (r *Registry[T]) Create(name string, config map[string]string) (T, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()

	if !ok {
		var zero T
		return zero, fmt.Errorf("unknown backend %q", name)
	}

	return factory(config)
}

// Has returns true if the named factory exists.
func (r *Registry[T]) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[name]
	return ok
}

// List returns all registered factory names.
func (r *Registry[T]) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

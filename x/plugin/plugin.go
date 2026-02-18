// Package plugin provides a generic, thread-safe registry for plugin factory
// functions. It enables off-tree implementations to be registered and
// retrieved at runtime.
//
// Each plugin entry is identified by a [Key] value, which is a lightweight
// value type parameterized on the Go type it manages. This means a factory for
// type A cannot be registered under a Key meant for type B — the compiler
// will reject it.
//
// The registry freezes automatically on the first call to [Get] for a given
// key: all registrations must happen before the first resolution (typically
// during package init).
package plugin

import (
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrFrozen is returned by [Register] when the plugin entry has already
	// been resolved via [Get] and can no longer accept new registrations.
	ErrFrozen = errors.New("plugin registry is frozen")
	// ErrNotFound is returned by [Get] when no factory has been registered
	// for the requested plugin key.
	ErrNotFound = errors.New("plugin not found")
	// ErrNilFactory is returned by [Register] when a nil factory is provided.
	ErrNilFactory = errors.New("factory must not be nil")
)

var (
	mu      sync.RWMutex
	entries = map[Name]*entry{}
)

// Name represents the Plugin name.
type Name string

// Register sets the factory for the given key.
// It returns [ErrFrozen] if the key has already been resolved (via [Get]),
// or [ErrNilFactory] if factory is nil.
// Calling Register again on the same key replaces the previous factory.
func Register[T any](key key[T], factory func() T) error {
	mu.Lock()
	defer mu.Unlock()

	e := entries[key.name]
	if e == nil {
		return fmt.Errorf("plugin: uninitialized key %q", key.name)
	}

	if e.frozen {
		return fmt.Errorf("%w: cannot register %q", ErrFrozen, key.name)
	}

	if factory == nil {
		return ErrNilFactory
	}

	e.factory = factory
	return nil
}

// Get calls the factory registered under key and returns a new T.
// The first call to Get for a given key freezes that plugin entry,
// preventing further registrations.
// It returns [ErrNotFound] if no factory has been registered for the key.
func Get[T any](key key[T]) (T, error) {
	mu.Lock()
	e := entries[key.name]
	if e == nil {
		mu.Unlock()
		var zero T
		return zero, fmt.Errorf("plugin: uninitialized key %q", key.name)
	}

	e.frozen = true
	f := e.factory
	mu.Unlock()

	if f == nil {
		var zero T
		return zero, fmt.Errorf("%w: %q", ErrNotFound, key.name)
	}
	return f.(func() T)(), nil
}

// Has reports whether a plugin has been registered for the given key.
func Has[T any](key key[T]) bool {
	mu.RLock()
	defer mu.RUnlock()

	e := entries[key.name]
	return e != nil && e.factory != nil
}

// Key identifies a typed plugin entry.
// It is a value type — safe to copy, cannot be nil.
type key[T any] struct {
	name Name
}

// newKey creates a new plugin entry key with the given name.
// It panics if a key with the same name has already been created.
func newKey[T any](name Name) key[T] {
	mu.Lock()
	defer mu.Unlock()

	if _, exists := entries[name]; exists {
		panic("plugin: duplicate key name: " + name)
	}
	entries[name] = &entry{}
	return key[T]{name: name}
}

// entry holds the internal state for a single plugin entry.
type entry struct {
	frozen  bool
	factory any // func() T stored as any; nil means not registered
}

// resetEntry clears the factory and unfreezes the plugin entry identified by
// name, restoring it to its initial state. It is intended for use in tests only.
func resetEntry(name Name) {
	mu.Lock()
	defer mu.Unlock()

	if e := entries[name]; e != nil {
		e.frozen = false
		e.factory = nil
	}
}

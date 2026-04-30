package oidmap

import (
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
)

// Memory is an in-memory implementation of Map.
type Memory struct {
	mu             sync.RWMutex
	nativeToCompat map[plumbing.Hash]plumbing.Hash
	compatToNative map[plumbing.Hash]plumbing.Hash
}

// NewMemory returns a new empty in-memory Map.
func NewMemory() *Memory {
	return &Memory{
		nativeToCompat: make(map[plumbing.Hash]plumbing.Hash),
		compatToNative: make(map[plumbing.Hash]plumbing.Hash),
	}
}

// ToCompat returns the compat hash for a native hash.
func (m *Memory) ToCompat(native plumbing.Hash) (plumbing.Hash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.nativeToCompat[native]
	if !ok {
		return plumbing.Hash{}, plumbing.ErrObjectNotFound
	}
	return h, nil
}

// ToNative returns the native hash for a compat hash.
func (m *Memory) ToNative(compat plumbing.Hash) (plumbing.Hash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.compatToNative[compat]
	if !ok {
		return plumbing.Hash{}, plumbing.ErrObjectNotFound
	}
	return h, nil
}

// Add records or replaces a native/compat hash mapping in memory.
func (m *Memory) Add(native, compat plumbing.Hash) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	setMapping(m.nativeToCompat, m.compatToNative, native, compat)
	return nil
}

// Count returns the number of in-memory mappings.
func (m *Memory) Count() (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.nativeToCompat), nil
}

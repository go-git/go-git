package compat

import (
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
)

// MemoryMapping is an in-memory implementation of HashMapping.
type MemoryMapping struct {
	mu             sync.RWMutex
	nativeToCompat map[plumbing.Hash]plumbing.Hash
	compatToNative map[plumbing.Hash]plumbing.Hash
}

// NewMemoryMapping returns a new empty in-memory HashMapping.
func NewMemoryMapping() *MemoryMapping {
	return &MemoryMapping{
		nativeToCompat: make(map[plumbing.Hash]plumbing.Hash),
		compatToNative: make(map[plumbing.Hash]plumbing.Hash),
	}
}

func (m *MemoryMapping) NativeToCompat(native plumbing.Hash) (plumbing.Hash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.nativeToCompat[native]
	if !ok {
		return plumbing.Hash{}, plumbing.ErrObjectNotFound
	}
	return h, nil
}

func (m *MemoryMapping) CompatToNative(compat plumbing.Hash) (plumbing.Hash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.compatToNative[compat]
	if !ok {
		return plumbing.Hash{}, plumbing.ErrObjectNotFound
	}
	return h, nil
}

func (m *MemoryMapping) Add(native, compat plumbing.Hash) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if oldCompat, ok := m.nativeToCompat[native]; ok && oldCompat != compat {
		delete(m.compatToNative, oldCompat)
	}

	m.nativeToCompat[native] = compat
	m.compatToNative[compat] = native
	return nil
}

func (m *MemoryMapping) Count() (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.nativeToCompat), nil
}

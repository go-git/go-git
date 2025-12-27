package idxfile

import (
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
)

// offsetHashCache provides a thread-safe cache for offset->hash lookups.
// It supports both incremental caching (from FindOffset calls) and
// full map building (lazy, on first FindHash without revIndex).
type offsetHashCache struct {
	cache     map[int64]plumbing.Hash
	buildOnce sync.Once
	mu        sync.RWMutex
}

// Get retrieves a hash from the cache by offset.
// Returns the hash and true if found, or ZeroHash and false if not found.
func (c *offsetHashCache) Get(offset int64) (plumbing.Hash, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return plumbing.ZeroHash, false
	}
	hash, ok := c.cache[offset]
	return hash, ok
}

// Put stores a hash in the cache for the given offset.
// This is used for incremental caching during FindOffset calls.
func (c *offsetHashCache) Put(offset int64, hash plumbing.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		c.cache = make(map[int64]plumbing.Hash)
	}
	c.cache[offset] = hash
}

// BuildOnce builds the complete offset->hash map lazily using the provided builder function.
// The builder is only called once, even if called concurrently from multiple goroutines.
// Returns any error from the builder function.
func (c *offsetHashCache) BuildOnce(builder func() (map[int64]plumbing.Hash, error)) error {
	var buildErr error
	c.buildOnce.Do(func() {
		cache, err := builder()
		if err != nil {
			buildErr = err
			return
		}
		c.mu.Lock()
		c.cache = cache
		c.mu.Unlock()
	})
	return buildErr
}

// offsetIdxPosCache provides a thread-safe cache for offset->idxPos lookups.
// This cache is populated during binary search operations to allow subsequent
// FindHash calls to skip the binary search entirely.
type offsetIdxPosCache struct {
	cache map[int64]int
	mu    sync.RWMutex
}

// Get retrieves an idxPos from the cache by offset.
// Returns the idxPos and true if found, or 0 and false if not found.
func (c *offsetIdxPosCache) Get(offset int64) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return 0, false
	}
	pos, ok := c.cache[offset]
	return pos, ok
}

// Put stores an idxPos in the cache for the given offset.
func (c *offsetIdxPosCache) Put(offset int64, idxPos int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		c.cache = make(map[int64]int)
	}
	c.cache[offset] = idxPos
}

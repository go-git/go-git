package filesystem

import (
	"sync"
	"time"

	"github.com/go-git/go-git/v6/plumbing/format/index"
)

// IndexCache is a cache for the git index. Implementations must be safe
// for concurrent use.
type IndexCache interface {
	// Get returns the cached index when modTime and fileSize match the
	// stored values. On a miss it returns nil.
	Get(modTime time.Time, fileSize int64) *index.Index
	// Set stores idx together with the file stat values used for
	// subsequent invalidation checks.
	Set(idx *index.Index, modTime time.Time, fileSize int64)
	// Clear removes any cached index.
	Clear()
}

// statIndexCache is the default stat-based IndexCache.
type statIndexCache struct {
	mu       sync.RWMutex
	cached   *index.Index
	modTime  time.Time
	fileSize int64
}

// NewIndexCache returns an IndexCache that invalidates when the file's
// modification time or size changes.
func NewIndexCache() IndexCache {
	return &statIndexCache{}
}

func (c *statIndexCache) Get(modTime time.Time, fileSize int64) *index.Index {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cached != nil && modTime.Equal(c.modTime) && fileSize == c.fileSize {
		return c.cached
	}
	return nil
}

func (c *statIndexCache) Set(idx *index.Index, modTime time.Time, fileSize int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cached = idx
	c.modTime = modTime
	c.fileSize = fileSize
}

func (c *statIndexCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cached = nil
	c.modTime = time.Time{}
	c.fileSize = 0
}

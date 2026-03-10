package filesystem

import (
	"container/list"

	"github.com/go-git/go-git/v6/plumbing"
)

const defaultDeltaCacheMaxBytes = 32 << 20

// deltaBaseKey identifies one cached delta base by pack hash and offset.
type deltaBaseKey struct {
	pack   plumbing.Hash
	offset int64
}

// deltaBaseEntry stores one cached resolved delta base.
type deltaBaseEntry struct {
	key     deltaBaseKey
	typ     plumbing.ObjectType
	content []byte
}

// deltaBaseCache is a weighted LRU cache for resolved delta base objects.
type deltaBaseCache struct {
	maxSize    int64
	actualSize int64
	ll         *list.List
	cache      map[deltaBaseKey]*list.Element
}

func newDeltaBaseCache(maxSize int64) *deltaBaseCache {
	return &deltaBaseCache{
		maxSize: maxSize,
		ll:      list.New(),
		cache:   make(map[deltaBaseKey]*list.Element),
	}
}

// get returns a cloned cached delta base at the given key.
func (c *deltaBaseCache) get(key deltaBaseKey) (plumbing.ObjectType, []byte, bool) {
	ee, ok := c.cache[key]
	if !ok {
		return 0, nil, false
	}
	c.ll.MoveToFront(ee)
	entry := ee.Value.(deltaBaseEntry)
	// TODO for both projects: is this actually needed?
	clone := make([]byte, len(entry.content))
	copy(clone, entry.content)
	return entry.typ, clone, true
}

// put stores a cloned delta base value at the given key.
func (c *deltaBaseCache) put(key deltaBaseKey, typ plumbing.ObjectType, content []byte) {
	entrySize := int64(len(content))
	if entrySize > c.maxSize {
		return
	}

	// TODO for both projects: is this actually needed?
	clone := make([]byte, len(content))
	copy(clone, content)

	if ee, ok := c.cache[key]; ok {
		oldEntry := ee.Value.(deltaBaseEntry)
		c.actualSize -= int64(len(oldEntry.content))
		c.ll.MoveToFront(ee)
		ee.Value = deltaBaseEntry{key: key, typ: typ, content: clone}
	} else {
		ee := c.ll.PushFront(deltaBaseEntry{key: key, typ: typ, content: clone})
		c.cache[key] = ee
	}

	c.actualSize += entrySize
	for c.actualSize > c.maxSize && c.ll.Len() > 0 {
		last := c.ll.Back()
		lastEntry := last.Value.(deltaBaseEntry)
		c.actualSize -= int64(len(lastEntry.content))
		c.ll.Remove(last)
		delete(c.cache, lastEntry.key)
	}
}

// clear removes all cached entries.
func (c *deltaBaseCache) clear() {
	c.ll = list.New()
	c.cache = make(map[deltaBaseKey]*list.Element)
	c.actualSize = 0
}

package packfile

import (
	"slices"

	"github.com/go-git/go-git/v6/plumbing"
)

// maxObjectsPrealloc caps the up-front capacity reserved from the pack's
// declared object count, so a header advertising an absurd quantity cannot
// trigger a multi-gigabyte allocation. The slice and maps still grow
// organically beyond this hint.
const maxObjectsPrealloc = 1 << 16 // 64 Ki entries

func newParserCache() *parserCache {
	c := &parserCache{}
	return c
}

// parserCache defines the cache used within the parser.
// This is not thread safe by itself, and relies on the parser to
// enforce thread-safety.
type parserCache struct {
	oi         []*ObjectHeader
	oiByHash   map[plumbing.Hash]*ObjectHeader
	oiByOffset map[int64]*ObjectHeader
}

func (c *parserCache) Add(oh *ObjectHeader) {
	c.oiByHash[oh.Hash] = oh
	c.oiByOffset[oh.Offset] = oh
	c.oi = append(c.oi, oh)
}

func (c *parserCache) Reset(n int) {
	hint := min(max(n, 0), maxObjectsPrealloc)
	if c.oi == nil {
		c.oi = make([]*ObjectHeader, 0, hint)
		c.oiByHash = make(map[plumbing.Hash]*ObjectHeader, hint)
		c.oiByOffset = make(map[int64]*ObjectHeader, hint)
	} else {
		c.oi = c.oi[:0]
		c.oi = slices.Grow(c.oi, hint)

		clear(c.oiByHash)
		clear(c.oiByOffset)
	}
}

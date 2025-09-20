package packfile

import (
	"slices"

	"github.com/go-git/go-git/v6/plumbing"
)

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
	if c.oi == nil {
		c.oi = make([]*ObjectHeader, 0, n)
		c.oiByHash = make(map[plumbing.Hash]*ObjectHeader, n)
		c.oiByOffset = make(map[int64]*ObjectHeader, n)
	} else {
		c.oi = c.oi[:0]
		c.oi = slices.Grow(c.oi, n)

		clear(c.oiByHash)
		clear(c.oiByOffset)
	}
}

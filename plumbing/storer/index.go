package storer

import "github.com/go-git/go-git/v6/plumbing/format/index"

// IndexStorer generic storage of index.Index
type IndexStorer interface {
	// SetIndex stores the index. Its Cache, the cache-tree, may be stale:
	// it describes the entries the index was read with, and the caller may
	// have changed them since. go-git clears it before storing an index
	// whose entries it changed, and a storer should not keep a Cache it
	// cannot vouch for.
	SetIndex(*index.Index) error
	// Index returns the stored index.
	Index() (*index.Index, error)
}

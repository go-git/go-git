package storer

import "github.com/go-git/go-git/v6/plumbing/format/index"

// IndexStorer generic storage of index.Index
type IndexStorer interface {
	// SetIndex stores the index. go-git prunes its Cache, the cache-tree,
	// of every entry that no longer describes the entries before it stores
	// an index it changed, so a storer may keep it. A storer that is also
	// called from outside go-git should call PruneCache on what it keeps or
	// writes, as the filesystem and memory storages do.
	SetIndex(*index.Index) error
	// Index returns the stored index.
	Index() (*index.Index, error)
}

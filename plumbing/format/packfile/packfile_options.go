package packfile

import (
	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
)

type PackfileOption func(*Packfile)

// WithCache sets the cache to be used throughout Packfile operations.
// Use this to share existing caches with the Packfile. If not used, a
// new cache instance will be created.
func WithCache(cache cache.Object) PackfileOption {
	return func(p *Packfile) {
		p.cache = cache
	}
}

// WithIdx sets the idxfile for the packfile.
func WithIdx(idx idxfile.Index) PackfileOption {
	return func(p *Packfile) {
		p.Index = idx
	}
}

// WithFs sets the filesystem to be used.
func WithFs(fs billy.Filesystem) PackfileOption {
	return func(p *Packfile) {
		p.fs = fs
	}
}

package packfile

import (
	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

// PackfileOption configures a Packfile.
type PackfileOption interface { //nolint:revive // stutters but is a well-established name
	applyPackfile(*Packfile)
}

type packfileOptionFunc func(*Packfile)

func (f packfileOptionFunc) applyPackfile(p *Packfile) { f(p) }

// WithCache sets the cache to be used throughout Packfile operations.
// Use this to share existing caches with the Packfile. If not used, a
// new cache instance will be created.
func WithCache(cache cache.Object) PackfileOption {
	return packfileOptionFunc(func(p *Packfile) {
		p.cache = cache
	})
}

// WithIdx sets the idxfile for the packfile.
func WithIdx(idx idxfile.Index) PackfileOption {
	return packfileOptionFunc(func(p *Packfile) {
		p.Index = idx
	})
}

// WithFs sets the filesystem to be used.
func WithFs(fs billy.Filesystem) PackfileOption {
	return packfileOptionFunc(func(p *Packfile) {
		p.fs = fs
	})
}

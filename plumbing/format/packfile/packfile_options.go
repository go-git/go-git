package packfile

import (
	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
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

// WithObjectIDSize sets the size of the object IDs inside the packfile.
// Valid options are hash.SHA1Size and hash.SHA256Size.
//
// When no object ID size is set, hash.SHA1Size will be used.
func WithObjectIDSize(sz int) PackfileOption {
	return func(p *Packfile) {
		p.objectIdSize = sz
	}
}

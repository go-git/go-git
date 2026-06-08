package packhandle

import (
	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/sharedfile"
)

// ReadAtCloser is the file shape returned by Source.Open.
// It is an alias for [sharedfile.ReadAtCloser]; both names
// refer to the same type at compile time.
type ReadAtCloser = sharedfile.ReadAtCloser

// Source describes how to obtain one file of a pack triple. Open
// is invoked lazily on first need and again after each
// grace-period close; Size is invoked lazily on demand and is
// expected to be cheap (typically backed by an [io/fs.Stat]).
type Source struct {
	// Open returns a fresh, independently closeable random-access
	// read handle.
	Open func() (ReadAtCloser, error)
	// Size returns the file's size in bytes.
	Size func() (int64, error)
}

// Sources bundles the three files of one pack. Pack is required.
// Idx and Rev are optional: when left zero, [PackHandle.Index]
// returns [ErrSourceUnconfigured].
type Sources struct {
	Pack Source
	Idx  Source
	Rev  Source
}

// PathSource constructs a [Source] backed by the given path on
// fs. Open delegates to fs.Open; Size delegates to fs.Stat.
func PathSource(fs billy.Basic, path string) Source {
	return Source{
		Open: func() (ReadAtCloser, error) {
			return fs.Open(path)
		},
		Size: func() (int64, error) {
			info, err := fs.Stat(path)
			if err != nil {
				return 0, err
			}
			return info.Size(), nil
		},
	}
}

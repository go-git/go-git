package packhandle

import (
	"io"

	billy "github.com/go-git/go-billy/v6"
)

// ReadAtCloser is the minimal contract on files returned by a
// [Source]: random-access reads via [io.ReaderAt], plus
// [io.Reader] and [io.Closer]. [billy.File] satisfies it
// implicitly.
type ReadAtCloser interface {
	io.ReaderAt
	io.ReadCloser
}

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

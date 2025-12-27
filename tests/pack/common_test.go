package pack_test

import (
	"crypto"
	"io"

	"github.com/go-git/go-billy/v6"

	_ "unsafe"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/revfile"
)

// The existing implementation uses int64, instead of uint64, which is
// the appropriate type to represent offsets. To limit the amount of changes
// this generic interface will be used to enable both types being represented.
// In the future, the use of int64 will need to be replaced by uint64.
type int64OrUint64 interface {
	~int64 | ~uint64
}

type packHandler[T int64OrUint64] interface {
	io.Closer

	FindOffset(h plumbing.Hash) (T, error)
	FindHash(offset T) (plumbing.Hash, error)
	Get(h plumbing.Hash) (plumbing.EncodedObject, error)
	GetByOffset(offset T) (plumbing.EncodedObject, error)
}

// newPackfileOpts creates a packfile handler using MemoryIndex (legacy path).
func newPackfileOpts(pack, idx billy.File, opts ...packfile.PackfileOption) packHandler[int64] {
	i := idxfile.NewMemoryIndex(crypto.SHA1.Size())

	_, err := pack.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}
	_, err = idx.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}

	err = idxfile.NewDecoder(idx).Decode(i)
	if err != nil {
		panic(err)
	}

	opts = append(opts, packfile.WithIdx(i))
	return packfile.NewPackfile(pack, opts...)
}

// newPackfileReaderAt creates a packfile handler using ReaderAtIndex with reverse index (efficient path).
func newPackfileReaderAt(pack, idx, rev billy.File, opts ...packfile.PackfileOption) packHandler[int64] {
	_, err := pack.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}

	// Create ReaderAtIndex from idx file
	i, err := idxfile.NewReaderAtIndex(idx, crypto.SHA1.Size())
	if err != nil {
		panic(err)
	}

	// Set up reverse index if available
	if rev != nil {
		count, err := i.Count()
		if err != nil {
			panic(err)
		}
		revIdx, err := revfile.NewReaderAtRevIndex(rev, crypto.SHA1.Size(), count)
		if err != nil {
			panic(err)
		}
		i.SetRevIndex(revIdx)
	}

	opts = append(opts, packfile.WithIdx(i))
	return packfile.NewPackfile(pack, opts...)
}

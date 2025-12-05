package pack_test

import (
	"bufio"
	"crypto"
	"io"
	"sync"

	"github.com/go-git/go-billy/v6"

	_ "unsafe"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
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

//go:linkname bufioPool github.com/go-git/go-git/v6/utils/sync.bufioReader
var bufioPool sync.Pool

// resetGlobalSyncPools resets the global sync pools. This is needed as
// the Suite execution ends up copying global vars by value, which in this
// case specifically results in the `New` becoming nil.
func resetGlobalSyncPools() {
	bufioPool = sync.Pool{New: func() any {
		return bufio.NewReader(nil)
	}}
}

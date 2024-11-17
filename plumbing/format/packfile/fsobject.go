package packfile

import (
	"io"

	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/utils/sync"
)

// FSObject is an object from the packfile on the filesystem.
type FSObject struct {
	hash   plumbing.Hash
	offset int64
	size   int64
	typ    plumbing.ObjectType
	index  idxfile.Index
	fs     billy.Filesystem
	path   string
	cache  cache.Object
}

// NewFSObject creates a new filesystem object.
func NewFSObject(
	hash plumbing.Hash,
	finalType plumbing.ObjectType,
	offset int64,
	contentSize int64,
	index idxfile.Index,
	fs billy.Filesystem,
	path string,
	cache cache.Object,
) *FSObject {
	return &FSObject{
		hash:   hash,
		offset: offset,
		size:   contentSize,
		typ:    finalType,
		index:  index,
		fs:     fs,
		path:   path,
		cache:  cache,
	}
}

// Reader implements the plumbing.EncodedObject interface.
func (o *FSObject) Reader() (io.ReadCloser, error) {
	obj, ok := o.cache.Get(o.hash)
	if ok && obj != o {
		reader, err := obj.Reader()
		if err != nil {
			return nil, err
		}

		return reader, nil
	}

	f, err := o.fs.Open(o.path)
	if err != nil {
		return nil, err
	}

	_, err = f.Seek(o.offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	dict := sync.GetByteSlice()
	zr := sync.NewZlibReader(dict)
	err = zr.Reset(f)
	if err != nil {
		return nil, err
	}
	return &zlibReadCloser{zr, dict}, nil
}

type zlibReadCloser struct {
	r    sync.ZLibReader
	dict *[]byte
}

// Read reads up to len(p) bytes into p from the data.
func (r *zlibReadCloser) Read(p []byte) (int, error) {
	return r.r.Reader.Read(p)
}

func (r *zlibReadCloser) Close() error {
	sync.PutByteSlice(r.dict)
	sync.PutZlibReader(r.r)
	return nil
}

// SetSize implements the plumbing.EncodedObject interface. This method
// is a noop.
func (o *FSObject) SetSize(int64) {}

// SetType implements the plumbing.EncodedObject interface. This method is
// a noop.
func (o *FSObject) SetType(plumbing.ObjectType) {}

// Hash implements the plumbing.EncodedObject interface.
func (o *FSObject) Hash() plumbing.Hash { return o.hash }

// Size implements the plumbing.EncodedObject interface.
func (o *FSObject) Size() int64 { return o.size }

// Type implements the plumbing.EncodedObject interface.
func (o *FSObject) Type() plumbing.ObjectType {
	return o.typ
}

// Writer implements the plumbing.EncodedObject interface. This method always
// returns a nil writer.
func (o *FSObject) Writer() (io.WriteCloser, error) {
	return nil, nil
}

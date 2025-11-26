package packfile

import (
	"bufio"
	"errors"
	"io"
	"os"

	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
)

// FSObject is an object from the packfile on the filesystem.
type FSObject struct {
	hash     plumbing.Hash
	offset   int64
	size     int64
	typ      plumbing.ObjectType
	index    idxfile.Index
	fs       billy.Filesystem
	pack     billy.File
	packPath string
	cache    cache.Object
}

// NewFSObject creates a new filesystem object.
func NewFSObject(
	hash plumbing.Hash,
	finalType plumbing.ObjectType,
	offset int64,
	contentSize int64,
	index idxfile.Index,
	fs billy.Filesystem,
	pack billy.File,
	packPath string,
	cache cache.Object,
) *FSObject {
	return &FSObject{
		hash:     hash,
		offset:   offset,
		size:     contentSize,
		typ:      finalType,
		index:    index,
		fs:       fs,
		pack:     pack,
		packPath: packPath,
		cache:    cache,
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

	var file io.Closer
	_, err := o.pack.Seek(o.offset, io.SeekStart)
	// fsobject aims to reuse an existing file descriptor to the packfile.
	// In some cases that descriptor would already be closed, in such cases,
	// open the packfile again and close it when the reader is closed.
	if err != nil && errors.Is(err, os.ErrClosed) {
		o.pack, err = o.fs.Open(o.packPath)
		if err != nil {
			return nil, err
		}
		file = o.pack
		_, err = o.pack.Seek(o.offset, io.SeekStart)
	}
	if err != nil {
		return nil, err
	}

	br := sync.GetBufioReader(o.pack)

	zr, err := sync.GetZlibReader(br)
	if err != nil {
		return nil, err
	}
	return &zlibReadCloser{r: zr, f: file, rbuf: br}, nil
}

type zlibReadCloser struct {
	r      *sync.ZLibReader
	f      io.Closer
	rbuf   *bufio.Reader
	closed bool
}

// Read reads up to len(p) bytes into p from the data.
func (r *zlibReadCloser) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (r *zlibReadCloser) Close() (err error) {
	if r.closed {
		return nil
	}
	r.closed = true

	if r.f != nil {
		defer ioutil.CheckClose(r.f, &err)
	}

	defer sync.PutBufioReader(r.rbuf)

	defer sync.PutZlibReader(r.r)
	return r.r.Close()
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

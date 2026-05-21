package packfile

import (
	"bufio"
	"errors"
	"io"
	"math"
	"os"

	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/packhandle"
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
	// acquireRandom, when set, supersedes pack/packPath/fs in
	// [FSObject.Reader]: each call yields a fresh cursor that
	// Close releases.
	acquireRandom func() (packhandle.RandomReader, error)
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
//
// Reader is safe for concurrent use: it uses ReadAt (which does not modify the
// file's seek cursor) instead of Seek+Read, so multiple goroutines can call
// Reader on FSObjects that share the same underlying packfile handle.
func (o *FSObject) Reader() (io.ReadCloser, error) {
	obj, ok := o.cache.Get(o.hash)
	if ok && obj != o {
		reader, err := obj.Reader()
		if err != nil {
			return nil, err
		}

		return reader, nil
	}

	var (
		pack io.ReaderAt
		file io.Closer
	)

	if o.acquireRandom != nil {
		cur, err := o.acquireRandom()
		if err != nil {
			return nil, err
		}
		pack = cur
		file = cur
	} else {
		pack = o.pack

		// Probe with a 1-byte ReadAt to detect a closed descriptor
		// without mutating shared state. A zero-length read is not
		// usable: some implementations (e.g. [os.File]) return
		// (0, nil) on a closed file.
		_, err := pack.ReadAt(make([]byte, 1), o.offset)
		if err != nil && errors.Is(err, os.ErrClosed) {
			reopened, oerr := o.fs.Open(o.packPath)
			if oerr != nil {
				return nil, oerr
			}
			pack = reopened
			file = reopened
		} else if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}

	// SectionReader provides a standalone io.Reader backed by ReadAt. Each
	// SectionReader maintains its own read position, so concurrent calls
	// to Reader do not interfere with each other or with the packfile's
	// Scanner. The upper bound is set to math.MaxInt64 because zlib
	// streams are self-terminating — the decompressor stops at the DEFLATE
	// end marker regardless of how many bytes remain available.
	sr := io.NewSectionReader(pack, o.offset, math.MaxInt64-o.offset)
	br := sync.GetBufioReader(sr)

	zr, err := sync.GetZlibReader(br)
	if err != nil {
		sync.PutBufioReader(br)
		if file != nil {
			_ = file.Close()
		}
		return nil, err
	}
	return NewBoundedReadCloser(&zlibReadCloser{r: zr, f: file, rbuf: br}, o.size), nil
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

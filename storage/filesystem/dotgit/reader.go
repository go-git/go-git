package dotgit

import (
	"fmt"
	"io"
	"os"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

var _ (plumbing.EncodedObject) = &EncodedObject{}

// EncodedObject is a read-only encoded object backed by the filesystem.
type EncodedObject struct {
	dir *DotGit
	h   plumbing.Hash
	t   plumbing.ObjectType
	sz  int64
}

// Hash returns the hash of the object.
func (e *EncodedObject) Hash() plumbing.Hash {
	return e.h
}

// Reader returns a reader for the object's contents.
func (e *EncodedObject) Reader() (io.ReadCloser, error) {
	f, err := e.dir.Object(e.h)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, plumbing.ErrObjectNotFound
		}

		return nil, err
	}
	r, err := objfile.NewReader(f, e.dir.options.ObjectFormat)
	if err != nil {
		return nil, err
	}

	t, size, err := r.Header()
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	if t != e.t {
		_ = r.Close()
		return nil, objfile.ErrHeader
	}
	if size != e.sz {
		_ = r.Close()
		return nil, objfile.ErrHeader
	}
	return ioutil.NewReadCloserWithCloser(r, f.Close), nil
}

// SetType is a no-op for read-only objects.
func (e *EncodedObject) SetType(plumbing.ObjectType) {}

// Type returns the object type.
func (e *EncodedObject) Type() plumbing.ObjectType {
	return e.t
}

// Size returns the object size in bytes.
func (e *EncodedObject) Size() int64 {
	return e.sz
}

// SetSize is a no-op for read-only objects.
func (e *EncodedObject) SetSize(int64) {}

// Writer returns an error because this object is read-only.
func (e *EncodedObject) Writer() (io.WriteCloser, error) {
	return nil, fmt.Errorf("not supported")
}

// NewEncodedObject creates a new read-only encoded object.
func NewEncodedObject(dir *DotGit, h plumbing.Hash, t plumbing.ObjectType, size int64) *EncodedObject {
	return &EncodedObject{
		dir: dir,
		h:   h,
		t:   t,
		sz:  size,
	}
}

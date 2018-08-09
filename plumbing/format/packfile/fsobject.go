package packfile

import (
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
)

// FSObject is an object from the packfile on the filesystem.
type FSObject struct {
	hash     plumbing.Hash
	h        *ObjectHeader
	offset   int64
	size     int64
	typ      plumbing.ObjectType
	packfile *Packfile
}

// NewFSObject creates a new filesystem object.
func NewFSObject(
	hash plumbing.Hash,
	finalType plumbing.ObjectType,
	offset int64,
	contentSize int64,
	packfile *Packfile,
) *FSObject {
	return &FSObject{
		hash:     hash,
		offset:   offset,
		size:     contentSize,
		typ:      finalType,
		packfile: packfile,
	}
}

// Reader implements the plumbing.EncodedObject interface.
func (o *FSObject) Reader() (io.ReadCloser, error) {
	return o.packfile.getObjectContent(o.offset)
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

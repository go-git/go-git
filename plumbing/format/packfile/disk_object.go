package packfile

import (
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
)

// DiskObject is an object from the packfile on disk.
type DiskObject struct {
	hash     plumbing.Hash
	h        *ObjectHeader
	offset   int64
	size     int64
	typ      plumbing.ObjectType
	packfile *Packfile
}

// NewDiskObject creates a new disk object.
func NewDiskObject(
	hash plumbing.Hash,
	finalType plumbing.ObjectType,
	offset int64,
	contentSize int64,
	packfile *Packfile,
) *DiskObject {
	return &DiskObject{
		hash:     hash,
		offset:   offset,
		size:     contentSize,
		typ:      finalType,
		packfile: packfile,
	}
}

// Reader implements the plumbing.EncodedObject interface.
func (o *DiskObject) Reader() (io.ReadCloser, error) {
	return o.packfile.getObjectContent(o.offset)
}

// SetSize implements the plumbing.EncodedObject interface. This method
// is a noop.
func (o *DiskObject) SetSize(int64) {}

// SetType implements the plumbing.EncodedObject interface. This method is
// a noop.
func (o *DiskObject) SetType(plumbing.ObjectType) {}

// Hash implements the plumbing.EncodedObject interface.
func (o *DiskObject) Hash() plumbing.Hash { return o.hash }

// Size implements the plumbing.EncodedObject interface.
func (o *DiskObject) Size() int64 { return o.size }

// Type implements the plumbing.EncodedObject interface.
func (o *DiskObject) Type() plumbing.ObjectType {
	return o.typ
}

// Writer implements the plumbing.EncodedObject interface. This method always
// returns a nil writer.
func (o *DiskObject) Writer() (io.WriteCloser, error) {
	return nil, nil
}

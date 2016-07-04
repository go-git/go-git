package packfile

import (
	"io"

	"gopkg.in/src-d/go-git.v3/core"
)

// Stream implements ReadRecaller for the io.Reader of a packfile.  This
// implementation keeps all remembered objects referenced in maps for
// quick access.
type Stream struct {
	io.Reader
	count          int64
	offsetToObject map[int64]core.Object
	hashToObject   map[core.Hash]core.Object
}

// NewStream returns a new Stream that reads form r.
func NewStream(r io.Reader) *Stream {
	return &Stream{
		Reader:         r,
		count:          0,
		hashToObject:   make(map[core.Hash]core.Object, 0),
		offsetToObject: make(map[int64]core.Object, 0),
	}
}

// Read reads up to len(p) bytes into p.
func (r *Stream) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.count += int64(n)

	return
}

// ReadByte reads a byte.
func (r *Stream) ReadByte() (byte, error) {
	var p [1]byte
	_, err := r.Reader.Read(p[:])
	r.count++

	return p[0], err
}

// Offset returns the number of bytes read.
func (r *Stream) Offset() (int64, error) {
	return r.count, nil
}

// Remember stores references to the passed object to be used later by
// RecalByHash and RecallByOffset. It receives the object and the offset
// of its object entry in the packfile.
func (r *Stream) Remember(o int64, obj core.Object) error {
	h := obj.Hash()
	if _, ok := r.hashToObject[h]; ok {
		return ErrDuplicatedObject.AddDetails("with hash %s", h)
	}
	r.hashToObject[h] = obj

	if _, ok := r.offsetToObject[o]; ok {
		return ErrDuplicatedObject.AddDetails("with offset %d", o)
	}
	r.offsetToObject[o] = obj

	return nil
}

// ForgetAll forgets all previously remembered objects.
func (r *Stream) ForgetAll() {
	r.hashToObject = make(map[core.Hash]core.Object)
	r.offsetToObject = make(map[int64]core.Object)
}

// RecallByHash returns an object that has been previously Remember-ed by
// its hash.
func (r *Stream) RecallByHash(h core.Hash) (core.Object, error) {
	obj, ok := r.hashToObject[h]
	if !ok {
		return nil, ErrCannotRecall.AddDetails("by hash %s", h)
	}

	return obj, nil
}

// RecallByOffset returns an object that has been previously Remember-ed by
// the offset of its object entry in the packfile.
func (r *Stream) RecallByOffset(o int64) (core.Object, error) {
	obj, ok := r.offsetToObject[o]
	if !ok {
		return nil, ErrCannotRecall.AddDetails("no object found at offset %d", o)
	}

	return obj, nil
}

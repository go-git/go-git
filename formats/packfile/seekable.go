package packfile

import (
	"io"
	"os"

	"gopkg.in/src-d/go-git.v3/core"
)

// Seekable implements ReadRecaller for the io.ReadSeeker of a packfile.
// Remembering does not actually stores any reference to the remembered
// objects; the object offset is remebered instead and the packfile is
// read again everytime a recall operation is requested. This saves
// memory buy can be very slow if the associated io.ReadSeeker is slow
// (like a hard disk).
type Seekable struct {
	io.ReadSeeker
	HashToOffset map[core.Hash]int64
}

// NewSeekable returns a new Seekable that reads form r.
func NewSeekable(r io.ReadSeeker) *Seekable {
	return &Seekable{
		r,
		make(map[core.Hash]int64),
	}
}

// Read reads up to len(p) bytes into p.
func (r *Seekable) Read(p []byte) (int, error) {
	return r.ReadSeeker.Read(p)
}

// ReadByte reads a byte.
func (r *Seekable) ReadByte() (byte, error) {
	var p [1]byte
	_, err := r.ReadSeeker.Read(p[:])
	if err != nil {
		return 0, err
	}

	return p[0], nil
}

// Offset returns the offset for the next Read or ReadByte.
func (r *Seekable) Offset() (int64, error) {
	return r.Seek(0, os.SEEK_CUR)
}

// Remember stores the offset of the object and its hash, but not the
// object itself.  This implementation does not check for already stored
// offsets, as it is too expensive to build this information from an
// index every time a get operation is performed on the SeekableReadRecaller.
func (r *Seekable) Remember(o int64, obj core.Object) error {
	h := obj.Hash()
	if _, ok := r.HashToOffset[h]; ok {
		return ErrDuplicatedObject.AddDetails("with hash %s", h)
	}

	r.HashToOffset[h] = o

	return nil
}

// ForgetAll forgets all previously remembered objects.  For efficiency
// reasons RecallByOffset always find objects, even if they have been
// forgetted or were never remembered.
func (r *Seekable) ForgetAll() {
	r.HashToOffset = make(map[core.Hash]int64)
}

// RecallByHash returns the object for a given hash by looking for it again in
// the io.ReadeSeerker.
func (r *Seekable) RecallByHash(h core.Hash) (core.Object, error) {
	o, ok := r.HashToOffset[h]
	if !ok {
		return nil, ErrCannotRecall.AddDetails("hash not found: %s", h)
	}

	return r.RecallByOffset(o)
}

// RecallByOffset returns the object for a given offset by looking for it again in
// the io.ReadeSeerker. For efficiency reasons, this method always find objects by
// offset, even if they have not been remembered or if they have been forgetted.
func (r *Seekable) RecallByOffset(o int64) (obj core.Object, err error) {
	// remember current offset
	beforeJump, err := r.Offset()
	if err != nil {
		return nil, err
	}

	defer func() {
		// jump back
		_, seekErr := r.Seek(beforeJump, os.SEEK_SET)
		if err == nil {
			err = seekErr
		}
	}()

	// jump to requested offset
	_, err = r.Seek(o, os.SEEK_SET)
	if err != nil {
		return nil, err
	}

	return NewParser(r).ReadObject()
}

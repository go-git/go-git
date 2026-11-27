package packfile

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// rawObjectWriter returns a writer that stores an object of type typ and
// size sz in s once it is closed. It uses s's storer.RawObjectWriter when
// s implements it, and otherwise builds the object with NewEncodedObject
// and stores it with SetEncodedObject on Close.
func rawObjectWriter(s storer.EncodedObjectStorer, typ plumbing.ObjectType, sz int64) (io.WriteCloser, error) {
	if rw, ok := s.(storer.RawObjectWriter); ok {
		return rw.RawObjectWriter(typ, sz)
	}

	obj := s.NewEncodedObject()
	obj.SetType(typ)
	obj.SetSize(sz)

	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}

	return &encodedObjectWriter{WriteCloser: w, obj: obj, storage: s}, nil
}

// encodedObjectWriter writes to an object's Writer and stores the object
// when it is closed.
type encodedObjectWriter struct {
	io.WriteCloser
	obj     plumbing.EncodedObject
	storage storer.EncodedObjectStorer
}

// Close closes the object's writer and then stores the object. It returns
// the first error from either step.
func (w *encodedObjectWriter) Close() error {
	if err := w.WriteCloser.Close(); err != nil {
		return err
	}

	_, err := w.storage.SetEncodedObject(w.obj)
	return err
}

// discard closes the object's writer without storing the object.
func (w *encodedObjectWriter) discard() {
	_ = w.WriteCloser.Close()
}

// abortObjectWriter releases w after a failed write. A fallback writer is
// discarded so the partial object is not stored. Any other writer is
// closed, as a storer.RawObjectWriter offers no way to abort.
func abortObjectWriter(w io.WriteCloser) {
	if ew, ok := w.(*encodedObjectWriter); ok {
		ew.discard()
		return
	}
	_ = w.Close()
}

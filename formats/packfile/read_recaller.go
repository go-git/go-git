package packfile

import "gopkg.in/src-d/go-git.v3/core"

var (
	// ErrDuplicatedObject is returned by Remember if an object appears several
	// times in a packfile.
	ErrDuplicatedObject = NewError("duplicated object")
	// ErrCannotRecall is returned by RecallByOffset or RecallByHash if the object
	// to recall cannot be returned.
	ErrCannotRecall = NewError("cannot recall object")
)

// The ReadRecaller interface has all the functions needed by a packfile
// Parser to operate. We provide two very different implementations:
// Seekable and Stream.
type ReadRecaller interface {
	// Read reads up to len(p) bytes into p.
	Read(p []byte) (int, error)
	// ReadByte is needed because of these:
	// - https://github.com/golang/go/commit/7ba54d45732219af86bde9a5b73c145db82b70c6
	// - https://groups.google.com/forum/#!topic/golang-nuts/fWTRdHpt0QI
	// - https://gowalker.org/compress/zlib#NewReader
	ReadByte() (byte, error)
	// Offset returns the number of bytes parsed so far from the
	// packfile.
	Offset() (int64, error)
	// Remember ask the ReadRecaller to remember the offset and hash for
	// an object, so you can later call RecallByOffset and RecallByHash.
	Remember(int64, core.Object) error
	// ForgetAll forgets all previously remembered objects.
	ForgetAll()
	// RecallByOffset returns the previously processed object found at a
	// given offset.
	RecallByOffset(int64) (core.Object, error)
	// RecallByHash returns the previously processed object with the
	// given hash.
	RecallByHash(core.Hash) (core.Object, error)
}

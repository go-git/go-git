package packhandle

import (
	"errors"
	"io"
	"io/fs"
	"sync/atomic"
)

// PackReader supports streaming access (Read+Seek+Close).
// Declared here temporarily; the next change extracts it to pack_reader.go.
type PackReader interface {
	io.Reader
	io.Seeker
	io.Closer
}

// RandomReader supports random access (ReadAt+Close). Deliberately
// excludes io.Reader — callers wanting streaming use PackReader.
// Declared here temporarily; the next change extracts it to pack_reader.go.
type RandomReader interface {
	io.ReaderAt
	io.Closer
}

// cursorReader is the concrete reader returned by both
// [PackHandle.OpenPackReader] and [PackHandle.OpenRandomReader].
// Each cursor holds its own offset and one [sharedFile] reference
// that Close releases.
//
// Read and Seek mutate the cursor offset and are not safe to call
// concurrently on the same cursor. ReadAt is safe to call
// concurrently with itself.
type cursorReader struct {
	sf     *sharedFile
	file   ReadAtCloser
	size   int64
	offset int64
	closed atomic.Bool
}

func newCursorReader(sf *sharedFile, size int64) (*cursorReader, error) {
	f, err := sf.Acquire()
	if err != nil {
		return nil, err
	}
	return &cursorReader{sf: sf, file: f, size: size}, nil
}

func (c *cursorReader) Read(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, fs.ErrClosed
	}
	if c.offset >= c.size {
		return 0, io.EOF
	}
	n, err := c.file.ReadAt(p, c.offset)
	c.offset += int64(n)
	if errors.Is(err, io.EOF) && n > 0 {
		err = nil
	}
	return n, err
}

func (c *cursorReader) ReadAt(p []byte, off int64) (int, error) {
	if c.closed.Load() {
		return 0, fs.ErrClosed
	}
	return c.file.ReadAt(p, off)
}

func (c *cursorReader) Seek(offset int64, whence int) (int64, error) {
	if c.closed.Load() {
		return 0, fs.ErrClosed
	}
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = c.offset + offset
	case io.SeekEnd:
		abs = c.size + offset
	default:
		return 0, errors.New("packhandle: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("packhandle: negative seek position")
	}
	c.offset = abs
	return abs, nil
}

// Close releases the underlying [sharedFile] reference. Idempotent.
func (c *cursorReader) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	c.sf.Release()
	return nil
}

var (
	_ PackReader   = (*cursorReader)(nil)
	_ RandomReader = (*cursorReader)(nil)
)

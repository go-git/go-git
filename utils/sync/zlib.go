package sync

import (
	"bytes"
	"compress/zlib"
	"io"
	"sync"
)

var (
	zlibInitBytes = []byte{0x78, 0x9c, 0x01, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}
	zlibReader    = sync.Pool{
		New: func() any {
			r, _ := zlib.NewReader(bytes.NewReader(zlibInitBytes))
			return &ZLibReader{
				reader: r.(zlibReadCloser),
				dict:   nil,
			}
		},
	}
	zlibWriter = sync.Pool{
		New: func() any {
			return zlib.NewWriter(nil)
		},
	}
)

type zlibReadCloser interface {
	io.ReadCloser
	zlib.Resetter
}

// ZLibReader is a poolable zlib reader.
type ZLibReader struct {
	dict   *[]byte
	reader zlibReadCloser
}

// Read reads data from the zlib reader.
func (r *ZLibReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

// Close closes the zlib reader.
func (r *ZLibReader) Close() error {
	return r.reader.Close()
}

// GetZlibReader returns a ZLibReader that is managed by a sync.Pool.
// Returns a ZLibReader that is reset using a dictionary that is
// also managed by a sync.Pool.
//
// After use, the ZLibReader should be put back into the sync.Pool
// by calling PutZlibReader.
func GetZlibReader(r io.Reader) (*ZLibReader, error) {
	z := zlibReader.Get().(*ZLibReader)
	z.dict = GetByteSlice()

	err := z.reader.Reset(r, *z.dict)

	return z, err
}

// PutZlibReader puts z back into its sync.Pool.
// The Byte slice dictionary is also put back into its sync.Pool.
func PutZlibReader(z *ZLibReader) {
	PutByteSlice(z.dict)
	zlibReader.Put(z)
}

// GetZlibWriter returns a *zlib.Writer that is managed by a sync.Pool.
// Returns a writer that is reset with w and ready for use.
//
// After use, the *zlib.Writer should be put back into the sync.Pool
// by calling PutZlibWriter.
func GetZlibWriter(w io.Writer) *zlib.Writer {
	z := zlibWriter.Get().(*zlib.Writer)
	z.Reset(w)
	return z
}

// PutZlibWriter puts w back into its sync.Pool.
func PutZlibWriter(w *zlib.Writer) {
	zlibWriter.Put(w)
}

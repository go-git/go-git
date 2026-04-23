package sync

import (
	"bytes"
	"io"
	"sync"

	"github.com/go-git/go-git/v6/x/plugin"
)

// ZlibReader is an alias of plugin.ZlibReader.
type ZlibReader = plugin.ZlibReader

// ZlibWriter is an alias of plugin.ZlibWriter.
type ZlibWriter = plugin.ZlibWriter

var (
	zlibInitBytes = []byte{0x78, 0x9c, 0x01, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}

	resolvedProvider = sync.OnceValue(func() plugin.ZlibProvider {
		p, err := plugin.Get(plugin.Zlib())
		if err != nil {
			// Unreachable in normal builds: x/plugin registers a
			// stdlib default in init.
			panic("utils/sync: no zlib provider registered in x/plugin: " + err.Error())
		}
		return p
	})

	zlibReader = sync.Pool{New: newPooledZlibReader}
	zlibWriter = sync.Pool{New: newPooledZlibWriter}
)

func newPooledZlibReader() any {
	r, err := resolvedProvider().NewReader(bytes.NewReader(zlibInitBytes))
	if err != nil {
		// Unreachable for any conforming zlib implementation:
		// zlibInitBytes is a minimal valid zlib stream used to seed
		// the pool. A provider returning an error here is
		// non-compliant; fail loudly rather than stash a nil reader
		// that would panic with no context on first use.
		panic("utils/sync: zlib provider failed to initialize pooled reader: " + err.Error())
	}
	return &ZLibReader{reader: r, dict: nil}
}

func newPooledZlibWriter() any {
	return resolvedProvider().NewWriter(nil)
}

// NewZlibWriter returns a ZlibWriter built by the active provider. The
// writer is not managed by a sync.Pool; the caller owns it for its
// full lifetime. Use this for long-lived writers (for example, one
// held per encoder across many Reset calls) where pool rental offers
// no benefit.
func NewZlibWriter(w io.Writer) ZlibWriter {
	return resolvedProvider().NewWriter(w)
}

// ZLibReader is a poolable zlib reader.
type ZLibReader struct {
	dict   *[]byte
	reader ZlibReader
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
	if z == nil {
		return
	}
	PutByteSlice(z.dict)
	zlibReader.Put(z)
}

// GetZlibWriter returns a ZlibWriter that is managed by a sync.Pool.
// Returns a writer that is reset with w and ready for use.
//
// After use, the writer should be put back into the sync.Pool by
// calling PutZlibWriter.
func GetZlibWriter(w io.Writer) ZlibWriter {
	z := zlibWriter.Get().(ZlibWriter)
	z.Reset(w)
	return z
}

// PutZlibWriter puts w back into its sync.Pool.
func PutZlibWriter(w ZlibWriter) {
	if w == nil {
		return
	}
	zlibWriter.Put(w)
}

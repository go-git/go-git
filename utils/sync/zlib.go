package sync

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"github.com/go-git/go-git/v6/utils/trace"
	"github.com/go-git/go-git/v6/x/plugin"
	"github.com/go-git/go-git/v6/x/plugin/zlib"
)

// ZlibReader is an alias of plugin.ZlibReader.
type ZlibReader = plugin.ZlibReader

// ZlibWriter is an alias of plugin.ZlibWriter.
type ZlibWriter = plugin.ZlibWriter

var errNilZlibReader = errors.New("utils/sync: zlib reader source is nil")

var (
	zlibInitBytes = []byte{0x78, 0x9c, 0x01, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}

	zlibProviderOnce sync.Once
	zlibProvider     plugin.ZlibProvider

	zlibReader = sync.Pool{New: newPooledZlibReader}
	zlibWriter = sync.Pool{New: newPooledZlibWriter}
)

func getZlibProvider() plugin.ZlibProvider {
	zlibProviderOnce.Do(func() {
		p, err := plugin.Get(plugin.Zlib())
		if err != nil {
			// This code path should be unreachable, as the zlib plugin
			// is registered with a built-in implementation, and invalid
			// registrations are rejected.
			//
			// If for some reason a plugin is not found, fallback to
			// default implementation.
			zlibProvider = zlib.NewStdlib()
			trace.Internal.Print("zlib plugin not registered: fall back to built-in version")
			return
		}
		zlibProvider = p
	})

	return zlibProvider
}

func newPooledZlibReader() any {
	r, err := getZlibProvider().NewReader(bytes.NewReader(zlibInitBytes))
	if err != nil {
		panic("zlib provider failed to initialize pooled reader: " + err.Error())
	}
	return &ZLibReader{reader: r, dict: nil}
}

func newPooledZlibWriter() any {
	return getZlibProvider().NewWriter(nil)
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

// Reset resets the pooled zlib reader.
func (r *ZLibReader) Reset(in io.Reader, dict []byte) error {
	return r.reader.Reset(in, dict)
}

// GetZlibReader returns a ZlibReader that is managed by a sync.Pool.
// Returns a ZlibReader that is reset using a dictionary that is
// also managed by a sync.Pool.
//
// After use, the ZLibReader should be put back into the sync.Pool
// by calling PutZlibReader.
func GetZlibReader(r io.Reader) (*ZLibReader, error) {
	if r == nil {
		return nil, errNilZlibReader
	}

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
	if w == nil {
		w = io.Discard
	}

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

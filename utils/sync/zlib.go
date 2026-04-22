package sync

import (
	"bytes"
	"compress/zlib"
	"io"
	"sync"
	"sync/atomic"
)

var (
	zlibInitBytes = []byte{0x78, 0x9c, 0x01, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}

	zlibProvider atomic.Pointer[ZlibProvider]

	zlibReader = sync.Pool{
		New: func() any {
			r, _ := activeZlibProvider().NewReader(bytes.NewReader(zlibInitBytes))
			return &ZLibReader{
				reader: r,
				dict:   nil,
			}
		},
	}
	zlibWriter = sync.Pool{
		New: func() any {
			return activeZlibProvider().NewWriter(nil)
		},
	}
)

func init() {
	var p ZlibProvider = stdlibZlibProvider{}
	zlibProvider.Store(&p)
}

// ZlibReader is the method set required of a zlib decompression reader.
// It matches the value returned by compress/zlib.NewReader, which
// implements both io.ReadCloser and zlib.Resetter.
type ZlibReader interface {
	io.ReadCloser
	// Reset swaps the source and an optional preset dictionary without
	// reallocating, matching zlib.Resetter.
	Reset(r io.Reader, dict []byte) error
}

// ZlibWriter is the method set required of a zlib compression writer.
// It matches the stdlib *zlib.Writer.
type ZlibWriter interface {
	io.WriteCloser
	// Reset swaps the destination writer, matching *zlib.Writer.Reset.
	Reset(w io.Writer)
	// Flush writes any pending compressed data to the underlying
	// writer, matching *zlib.Writer.Flush.
	Flush() error
}

// ZlibProvider constructs zlib implementations. Go-git calls its
// factory methods (often via internal sync.Pool instances) whenever it
// needs to read or write zlib-compressed data. Install a non-default
// provider with SetZlibProvider — for example to swap in
// github.com/klauspost/compress/zlib without go-git taking a direct
// dependency on it.
//
// Implementations must be safe for concurrent calls to NewReader and
// NewWriter. Returned readers and writers need not be concurrency-safe
// themselves; each caller gets its own instance.
type ZlibProvider interface {
	NewReader(r io.Reader) (ZlibReader, error)
	NewWriter(w io.Writer) ZlibWriter
}

// SetZlibProvider installs p as the zlib implementation used by go-git
// and returns the provider that was previously active. The returned
// value is useful for save-and-restore patterns in tests.
//
// Call SetZlibProvider during program init, before any go-git
// operation runs. Installing a provider after go-git has begun
// compressing or decompressing data is undefined: existing sync.Pool
// entries remain on the old provider until the garbage collector
// evicts them, and callers that have already acquired a reader or
// writer continue to use the old one.
func SetZlibProvider(p ZlibProvider) ZlibProvider {
	prev := zlibProvider.Swap(&p)
	if prev == nil {
		return nil
	}
	return *prev
}

func activeZlibProvider() ZlibProvider {
	return *zlibProvider.Load()
}

// stdlibZlibProvider produces compress/zlib readers and writers and is
// the default provider registered in init.
type stdlibZlibProvider struct{}

func (stdlibZlibProvider) NewReader(r io.Reader) (ZlibReader, error) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}
	// compress/zlib's reader value implements zlib.Resetter, so its
	// concrete type satisfies ZlibReader.
	return zr.(ZlibReader), nil
}

func (stdlibZlibProvider) NewWriter(w io.Writer) ZlibWriter {
	return zlib.NewWriter(w)
}

// NewZlibWriter returns a ZlibWriter built by the active provider. The
// writer is not managed by a sync.Pool; the caller owns it for its
// full lifetime. Use this for long-lived writers (for example, one
// held per encoder across many Reset calls) where pool rental offers
// no benefit.
func NewZlibWriter(w io.Writer) ZlibWriter {
	return activeZlibProvider().NewWriter(w)
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

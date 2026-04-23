// Package zlib provides the zlib provider interfaces and the built-in
// Stdlib implementation backing go-git's default compression. The
// interfaces live here — rather than in x/plugin — so x/plugin can
// alias them without creating an import cycle back to this subpackage.
package zlib

import (
	"compress/zlib"
	"fmt"
	"io"
)

// Reader is the method set required of a zlib decompression reader.
// It matches the value returned by compress/zlib.NewReader, which
// implements both io.ReadCloser and zlib.Resetter.
//
// Implementations must support Reset being called on a reader after
// Close so that pooled readers can be re-seeded with a new source and
// dictionary, matching zlib.Resetter semantics. Reset runs once per
// packfile object during scan, so it should be O(1) and avoid
// reallocating decompressor state.
type Reader interface {
	io.ReadCloser
	Reset(r io.Reader, dict []byte) error
}

// Writer is the method set required of a zlib compression writer.
// It matches the stdlib *zlib.Writer.
//
// Implementations must preserve the following behavioral contract
// that go-git relies on:
//
//   - Close flushes pending data and writes the zlib stream footer,
//     but must not close the underlying io.Writer. Both objfile and
//     packfile keep using the wrapped writer after the ZlibWriter
//     closes.
//   - Reset after Close is supported: packfile.Encoder closes the
//     writer once per object entry and then calls Reset to reuse it
//     for the next entry within the same encode. Reset is on the
//     per-object hot path during encode, so it should be O(1) and
//     avoid reallocating compressor state.
//   - Flush writes any pending compressed data to the underlying
//     writer without ending the stream; the writer remains usable
//     after Flush.
type Writer interface {
	io.WriteCloser
	Reset(w io.Writer)
	Flush() error
}

// Provider constructs zlib implementations. go-git calls its factory
// methods (often via internal sync.Pool instances) whenever it needs
// to read or write zlib-compressed data. Register a non-default
// provider with plugin.Register(plugin.Zlib(), factory) — for example
// to swap in github.com/klauspost/compress/zlib without go-git taking
// a direct dependency on it.
//
// Implementations must be safe for concurrent calls to NewReader and
// NewWriter. Returned readers and writers need not be concurrency-safe
// themselves; each caller gets its own instance.
type Provider interface {
	NewReader(r io.Reader) (Reader, error)
	NewWriter(w io.Writer) Writer
}

// Stdlib is a zlib provider backed by the Go standard library's
// compress/zlib package. It is the default provider registered with
// x/plugin during package init.
type Stdlib struct{}

// NewStdlib returns a new Stdlib provider.
func NewStdlib() *Stdlib {
	return &Stdlib{}
}

// NewReader returns a zlib decompression reader backed by compress/zlib.
func (*Stdlib) NewReader(r io.Reader) (Reader, error) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}
	zlr, ok := zr.(Reader)
	if !ok {
		return nil, fmt.Errorf("compress/zlib reader %T does not implement zlib.Resetter", zr)
	}
	return zlr, nil
}

// NewWriter returns a zlib compression writer backed by compress/zlib.
func (*Stdlib) NewWriter(w io.Writer) Writer {
	return zlib.NewWriter(w)
}

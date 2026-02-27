// Package ioutil implements some I/O utility functions.
package ioutil

import (
	"errors"
	"io"
)

// Peeker is an interface for types that can peek at the next bytes.
type Peeker interface {
	Peek(int) ([]byte, error)
}

// ReadPeeker is an interface that groups the basic Read and Peek methods.
type ReadPeeker interface {
	io.Reader
	Peeker
}

type (
	CloserFunc func() error
	WriterFunc func([]byte) (int, error)
	ReaderFunc func([]byte) (int, error)
)

func (f CloserFunc) Close() error                { return f() }
func (f WriterFunc) Write(p []byte) (int, error) { return f(p) }
func (f ReaderFunc) Read(p []byte) (int, error)  { return f(p) }

var (
	_ io.Closer = CloserFunc(nil)
	_ io.Writer = WriterFunc(nil)
	_ io.Reader = ReaderFunc(nil)
)

type multiCloser struct{ closers []io.Closer }

func (mc *multiCloser) Close() error {
	var errs []error

	for _, c := range mc.closers {
		if c == nil {
			continue
		}

		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// MultiCloser returns a closer that sequentailly closes
// given closers. The errors are merged via [errors.Join].
func MultiCloser(closers ...io.Closer) io.Closer {
	return &multiCloser{closers: closers}
}

type readCloser struct {
	io.Reader
	closer io.Closer
}

func (r *readCloser) Close() error {
	return r.closer.Close()
}

// NewReadCloser creates an `io.ReadCloser` with the given `io.Reader` and
// `io.Closer`.
func NewReadCloser(r io.Reader, c io.Closer) io.ReadCloser {
	return &readCloser{Reader: r, closer: c}
}

type writeCloser struct {
	io.Writer
	closer io.Closer
}

func (r *writeCloser) Close() error {
	if r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

// NewWriteCloser creates an `io.WriteCloser` with the given `io.Writer` and
// `io.Closer`.
func NewWriteCloser(w io.Writer, c io.Closer) io.WriteCloser {
	return &writeCloser{Writer: w, closer: c}
}

type writeNopCloser struct {
	io.Writer
}

func (writeNopCloser) Close() error { return nil }

// WriteNopCloser returns a WriteCloser with a no-op Close method wrapping
// the provided Writer w.
func WriteNopCloser(w io.Writer) io.WriteCloser {
	return writeNopCloser{w}
}

// CheckClose calls Close on the given io.Closer. If the given *error points to
// nil, it will be assigned the error returned by Close. Otherwise, any error
// returned by Close will be ignored. CheckClose is usually called with defer.
func CheckClose(c io.Closer, err *error) {
	if cerr := c.Close(); cerr != nil && *err == nil {
		*err = cerr
	}
}

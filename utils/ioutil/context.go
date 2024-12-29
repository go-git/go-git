package ioutil

import (
	"context"
	"io"
	"slices"
)

type ioret struct {
	err error
	n   int
}

type Writer interface {
	io.Writer
}

type ctxWriter struct {
	w   io.Writer
	ctx context.Context
}

// NewContextWriter wraps a writer to make it respect the given Context.
// If there is a blocking write, the returned Writer will return
// whenever the context is cancelled (the return values are n=0
// and err=ctx.Err().)
//
// Note that this wrapper DOES NOT ACTUALLY cancel the underlying
// write, as there is no way to do that with the standard Go io
// interface. So the read and write _will_ happen or hang. Use
// this sparingly, make sure to cancel the read or write as necessary
// (e.g. closing a connection whose context is up, etc.)
//
// Furthermore, in order to protect your memory from being read
// _after_ you've cancelled the context, this io.Writer will
// first make a **copy** of the buffer.
func NewContextWriter(ctx context.Context, w io.Writer) *ctxWriter {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ctxWriter{ctx: ctx, w: w}
}

func (w *ctxWriter) Write(buf []byte) (int, error) {
	buf2 := slices.Clone(buf)

	c := make(chan ioret, 1)

	go func() {
		n, err := w.w.Write(buf2)
		c <- ioret{err, n}
		close(c)
	}()

	select {
	case r := <-c:
		return r.n, r.err
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

type Reader interface {
	io.Reader
}

type ctxReader struct {
	r      io.Reader
	ctx    context.Context
	closer io.Closer
}

// NewContextReader wraps a reader to make it respect given Context.
// If there is a blocking read, the returned Reader will return
// whenever the context is cancelled (the return values are n=0
// and err=ctx.Err().)
//
// Note well: this wrapper DOES NOT ACTUALLY cancel the underlying
// write-- there is no way to do that with the standard go io
// interface. So the read and write _will_ happen or hang. So, use
// this sparingly, make sure to cancel the read or write as necesary
// (e.g. closing a connection whose context is up, etc.)
//
// Furthermore, in order to protect your memory from being read
// _before_ you've cancelled the context, this io.Reader will
// allocate a buffer of the same size, and **copy** into the client's
// if the read succeeds in time.
func NewContextReader(ctx context.Context, r io.Reader) *ctxReader {
	return &ctxReader{ctx: ctx, r: r}
}

func (r *ctxReader) Read(buf []byte) (int, error) {
	buf2 := make([]byte, len(buf))

	c := make(chan ioret, 1)

	go func() {
		n, err := r.r.Read(buf2)
		c <- ioret{err, n}
		close(c)
	}()

	select {
	case ret := <-c:
		copy(buf, buf2)
		return ret.n, ret.err
	case <-r.ctx.Done():
		if r.closer != nil {
			r.closer.Close()
		}
		return 0, r.ctx.Err()
	}
}

func NewContextReaderWithCloser(ctx context.Context, r io.Reader, closer io.Closer) *ctxReader {
	return &ctxReader{ctx: ctx, r: r, closer: closer}
}

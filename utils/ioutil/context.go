package ioutil

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/utils/sync"
)

type ioret struct {
	err error
	n   int
}

// Writer is an interface for io.Writer.
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
// use a buffer from the memory pool.
func NewContextWriter(ctx context.Context, w io.Writer) io.Writer {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ctxWriter{ctx: ctx, w: w}
}

func (w *ctxWriter) Write(buf []byte) (int, error) {
	ret := make(chan ioret, 1)
	input := make(chan []byte)
	defer close(input)

	// temp will be released when both goroutines stop using it.
	temp := sync.GetByteSlice()

	go func() {
		defer func() {
			if v := recover(); v != nil {
				err := fmt.Errorf("underlying writer resulted in panic: %v", v)
				ret <- ioret{err, 0}
			}

			sync.PutByteSlice(temp)
		}()

		for {
			buf2, ok := <-input
			if !ok {
				return
			}

			n, err := w.w.Write(buf2)
			ret <- ioret{err, n}
		}
	}()

	total := 0

	for len(buf) > 0 {
		n := copy(*temp, buf)
		input <- (*temp)[:n]

		select {
		case <-w.ctx.Done():
			return total, w.ctx.Err()
		case write := <-ret:
			if err := w.ctx.Err(); err != nil {
				return total, w.ctx.Err()
			}

			total += write.n
			buf = buf[write.n:]

			if write.err != nil {
				return total, write.err
			}
		}
	}

	return total, nil
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
// read-- there is no way to do that with the standard go io
// interface. So the read and write _will_ happen or hang. So, use
// this sparingly, make sure to cancel the read or write as necessary
// (e.g. closing a connection whose context is up, etc.)
//
// Furthermore, in order to protect your memory from being read
// _before_ you've cancelled the context, this io.Reader will
// use a buffer from the memory pool.
func NewContextReader(ctx context.Context, r io.Reader) io.Reader {
	return &ctxReader{ctx: ctx, r: r}
}

func (r *ctxReader) Read(buf []byte) (int, error) {
	ret := make(chan ioret, 1)

	temp := sync.GetByteSlice()
	window := (*temp)[:min(len(*temp), len(buf))]

	// temp will be released when both goroutines stop using it.
	done := make(chan struct{}, 1)
	defer close(done)

	go func() {
		defer func() {
			if v := recover(); v != nil {
				err := fmt.Errorf("underlying reader resulted in panic: %v", v)
				ret <- ioret{err, 0}
			}

			// wait for the main goroutine to copy from the buffer.
			<-done
			sync.PutByteSlice(temp)
		}()

		n, err := r.r.Read(window)
		ret <- ioret{err, n}
	}()

	select {
	case <-r.ctx.Done():
		if r.closer != nil {
			_ = r.closer.Close()
		}
		return 0, r.ctx.Err()
	case read := <-ret:
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		copy(buf, window[:read.n])
		return read.n, read.err
	}
}

// NewContextReaderWithCloser wraps a reader to make it respect given Context,
// and closes the closer when the context is done.
func NewContextReaderWithCloser(ctx context.Context, r io.Reader, closer io.Closer) io.Reader {
	return &ctxReader{ctx: ctx, r: r, closer: closer}
}

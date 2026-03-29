package ioutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	gogitsync "github.com/go-git/go-git/v6/utils/sync"
)

type ioret struct {
	err error
	n   int
}

type ctxWriter struct {
	w   io.Writer
	ctx context.Context

	mu     sync.Mutex
	buf    *[]byte
	input  chan []byte
	ret    chan ioret
	done   chan struct{}
	closed bool
}

// NewContextWriteCloser wraps a writer to make it respect the given Context.
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
// The callers MUST close this io.WriteCloser to free it's resources. it internally
// borrows a memory block from globally shared pool, and also spawns a goroutine.
func NewContextWriteCloser(ctx context.Context, w io.Writer) io.WriteCloser {
	if ctx == nil {
		ctx = context.Background()
	}

	ctxw := &ctxWriter{
		ctx:   ctx,
		w:     w,
		buf:   gogitsync.GetByteSlice(),
		ret:   make(chan ioret, 1),
		input: make(chan []byte),
		done:  make(chan struct{}),
	}

	go ctxw.writeLoop()

	return ctxw
}

func (w *ctxWriter) Write(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, errors.New("writer is closed")
	}

	select {
	case <-w.ctx.Done():
		// the context is closed before invoking this.
		return 0, w.ctx.Err()
	default:
	}

	total := 0

	for len(buf) > 0 {
		n := copy(*w.buf, buf)
		w.input <- (*w.buf)[:n]

		select {
		case <-w.ctx.Done():
			return total, w.ctx.Err()
		case write := <-w.ret:
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

func (w *ctxWriter) writeLoop() {
	defer func() {
		if v := recover(); v != nil {
			err := fmt.Errorf("underlying writer resulted in panic: %v", v)
			w.ret <- ioret{err, 0}
		}

		close(w.done)
	}()

	for buf := range w.input {
		n, err := w.w.Write(buf)
		w.ret <- ioret{err, n}
	}
}

func (w *ctxWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	close(w.input)
	<-w.done

	gogitsync.PutByteSlice(w.buf)
	w.buf = nil
	w.closed = true

	return nil
}

type ctxReader struct {
	r   io.Reader
	ctx context.Context

	mu     sync.Mutex
	buf    *[]byte
	input  chan []byte
	ret    chan ioret
	done   chan struct{}
	closed bool
}

// NewContextReadCloser wraps a reader to make it respect given Context.
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
// The callers MUST close this io.ReadCloser to free it's resources. it internally
// borrows a memory block from globally shared pool, and also spawns a goroutine.
func NewContextReadCloser(ctx context.Context, r io.Reader) io.ReadCloser {
	if ctx == nil {
		ctx = context.Background()
	}

	ctxr := &ctxReader{
		ctx:   ctx,
		r:     r,
		buf:   gogitsync.GetByteSlice(),
		input: make(chan []byte),
		ret:   make(chan ioret, 1),
		done:  make(chan struct{}),
	}

	go ctxr.readLoop()

	return ctxr
}

func (r *ctxReader) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return 0, errors.New("reader is closed")
	}

	window := (*r.buf)[:min(len(*r.buf), len(buf))]

	select {
	case <-r.ctx.Done():
		// the context is closed before invoking this.
		return 0, r.ctx.Err()
	default:
	}

	r.input <- window

	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case read := <-r.ret:
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		copy(buf, window[:read.n])
		return read.n, read.err
	}
}

func (r *ctxReader) readLoop() {
	defer func() {
		if v := recover(); v != nil {
			err := fmt.Errorf("underlying reader resulted in panic: %v", v)
			r.ret <- ioret{err, 0}
		}

		close(r.done)
	}()

	for buf := range r.input {
		n, err := r.r.Read(buf)
		r.ret <- ioret{err, n}
	}
}

func (r *ctxReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	close(r.input)
	<-r.done

	gogitsync.PutByteSlice(r.buf)
	r.buf = nil
	r.closed = true

	return nil
}

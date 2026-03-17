package file

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// ErrBufferFull is returned when the pipe buffer exceeds its maximum size.
// This indicates a protocol issue where one side is writing faster than
// the other can read, which would cause deadlock with unbuffered pipes.
var ErrBufferFull = errors.New("pipe buffer full: would deadlock")

// maxBufferSize is the maximum buffer size for the pipe.
// 10MB is far more than needed for pack negotiation (typically <100KB).
const maxBufferSize = 10 * 1024 * 1024

// bufferedPipe creates a pipe with an intermediate buffer to prevent deadlocks.
// Unlike io.Pipe which has zero buffer (writes block until reads), this allows
// writes to proceed up to maxBufferSize before returning an error.
type bufferedPipe struct {
	buf    bytes.Buffer
	mu     sync.Mutex
	cond   *sync.Cond
	closed bool
	err    error
}

// bufferedPipeReader is the read half of a buffered pipe.
type bufferedPipeReader struct {
	p *bufferedPipe
}

// bufferedPipeWriter is the write half of a buffered pipe.
type bufferedPipeWriter struct {
	p *bufferedPipe
}

// newBufferedPipe creates a new buffered pipe pair.
func newBufferedPipe() (*bufferedPipeReader, *bufferedPipeWriter) {
	p := &bufferedPipe{}
	p.cond = sync.NewCond(&p.mu)
	return &bufferedPipeReader{p: p}, &bufferedPipeWriter{p: p}
}

// Read reads data from the pipe, blocking until data is available or the pipe is closed.
func (r *bufferedPipeReader) Read(data []byte) (n int, err error) {
	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	for r.p.buf.Len() == 0 && !r.p.closed && r.p.err == nil {
		r.p.cond.Wait()
	}

	if r.p.buf.Len() > 0 {
		return r.p.buf.Read(data)
	}

	if r.p.err != nil {
		return 0, r.p.err
	}

	return 0, io.EOF
}

// Close closes the read half of the pipe.
func (r *bufferedPipeReader) Close() error {
	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	if !r.p.closed {
		r.p.closed = true
		r.p.err = io.ErrClosedPipe
		r.p.cond.Broadcast()
	}
	return nil
}

// Write writes data to the pipe buffer.
// Returns ErrBufferFull if the buffer would exceed maxBufferSize.
func (w *bufferedPipeWriter) Write(data []byte) (n int, err error) {
	w.p.mu.Lock()
	defer w.p.mu.Unlock()

	if w.p.closed {
		return 0, io.ErrClosedPipe
	}

	if w.p.buf.Len()+len(data) > maxBufferSize {
		return 0, ErrBufferFull
	}

	n, err = w.p.buf.Write(data)
	w.p.cond.Broadcast()
	return n, err
}

// Close closes the write half of the pipe.
func (w *bufferedPipeWriter) Close() error {
	w.p.mu.Lock()
	defer w.p.mu.Unlock()

	if !w.p.closed {
		w.p.closed = true
		w.p.cond.Broadcast()
	}
	return nil
}

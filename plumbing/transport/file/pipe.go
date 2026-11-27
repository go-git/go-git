package file

import (
	"io"
	"sync"
)

// answerBufferSize is how much the server can write ahead of the client. A
// stateful client writes a round of haves before it has read the answer to
// the round before, so an answer has to fit; it is a few kilobytes at most.
const answerBufferSize = 64 * 1024

// bufferedPipe carries what the server writes to the client. Unlike an
// [io.Pipe], a write returns once the bytes are buffered, and blocks only
// while the buffer is full, so the server can answer while the client is
// still writing.
type bufferedPipe struct {
	mu       sync.Mutex
	readable sync.Cond
	writable sync.Cond

	buf []byte
	// writeErr is what a read returns once the buffer is drained: set when
	// the writing side is closed.
	writeErr error
	// readClosed says that nobody will read any more.
	readClosed bool
}

func newBufferedPipe() *bufferedPipe {
	p := &bufferedPipe{}
	p.readable.L = &p.mu
	p.writable.L = &p.mu
	return p
}

// Read returns buffered bytes, waiting for some to arrive. Once the writing
// side is closed and the buffer is empty it returns the error the side was
// closed with, io.EOF if none.
func (p *bufferedPipe) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for len(p.buf) == 0 {
		if p.readClosed {
			return 0, io.ErrClosedPipe
		}
		if p.writeErr != nil {
			return 0, p.writeErr
		}
		p.readable.Wait()
	}
	n := copy(b, p.buf)
	p.buf = p.buf[:copy(p.buf, p.buf[n:])]
	p.writable.Broadcast()
	return n, nil
}

// Write buffers b, waiting for room when the buffer is full. It returns
// io.ErrClosedPipe once either side is closed.
func (p *bufferedPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	written := 0
	for len(b) > 0 {
		for len(p.buf) >= answerBufferSize && !p.readClosed && p.writeErr == nil {
			p.writable.Wait()
		}
		if p.readClosed || p.writeErr != nil {
			return written, io.ErrClosedPipe
		}
		n := min(len(b), answerBufferSize-len(p.buf))
		p.buf = append(p.buf, b[:n]...)
		b = b[n:]
		written += n
		p.readable.Broadcast()
	}
	return written, nil
}

// CloseRead ends reading: pending and later writes fail.
func (p *bufferedPipe) CloseRead() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.readClosed = true
	p.buf = nil
	p.readable.Broadcast()
	p.writable.Broadcast()
	return nil
}

// CloseWrite ends writing. Reads return what is buffered and then err, or
// io.EOF if err is nil. Only the first call counts.
func (p *bufferedPipe) CloseWrite(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.writeErr != nil {
		return
	}
	if err == nil {
		err = io.EOF
	}
	p.writeErr = err
	p.readable.Broadcast()
	p.writable.Broadcast()
}

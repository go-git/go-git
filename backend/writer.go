package backend

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
)

const defaultChunkSize = 4096

// flushResponseWriter wraps http.ResponseWriter to chunk output and
// flush after each write. Implements io.ReaderFrom and io.Closer.
type flushResponseWriter struct {
	http.ResponseWriter
	log       *log.Logger
	chunkSize int
	// started records whether the response status line has been committed
	// (first WriteHeader, or first Write — which implicitly commits a 200).
	// Once true, an error surfacing afterwards can only be logged:
	// renderStatusError would both race the concurrent writer and be unable to
	// change the already-sent status. While still false the caller may safely
	// render a real error status instead of an implicit 200.
	started bool
}

// WriteHeader records that the response has started, then delegates.
func (f *flushResponseWriter) WriteHeader(code int) {
	f.started = true
	f.ResponseWriter.WriteHeader(code)
}

// Write records that the response has started (the first Write commits a 200
// status if WriteHeader was not called), then delegates.
func (f *flushResponseWriter) Write(p []byte) (int, error) {
	f.started = true
	return f.ResponseWriter.Write(p)
}

// ReadFrom implements io.ReaderFrom.
func (f *flushResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	flusher := http.NewResponseController(f.ResponseWriter)

	var n int64
	p := make([]byte, f.chunkSize)
	for {
		nr, err := r.Read(p)
		if errors.Is(err, io.EOF) {
			break
		}
		nw, err := f.Write(p[:nr])
		if err != nil {
			// Body partially written; renderStatusError would race the writer.
			f.log.Printf("error writing response: %v", err)
			return n, err
		}
		if nr != nw {
			return n, err
		}
		n += int64(nr)
		if err := flusher.Flush(); err != nil {
			f.log.Printf("mismatched bytes written: expected %d, wrote %d", nr, nw)
			return n, fmt.Errorf("%w: error while flush", err)
		}
	}

	return n, nil
}

// Close implements io.Closer. It is a no-op.
func (f *flushResponseWriter) Close() error {
	return nil
}

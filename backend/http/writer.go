package http

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
)

// defaultChunkSize is the default chunk size for the flushResponseWriter.
const defaultChunkSize = 4096

// flushResponseWriter is a wrapper around http.ResponseWriter that handles
// buffered output. It chunks the output and flushes it to the client. It
// implements the io.ReaderFrom interface to read from an io.Reader and write
// to the ResponseWriter. It also implements a no-op Close method to satisfy
// the io.Closer interface.
// Useful when using proxies.
type flushResponseWriter struct {
	http.ResponseWriter
	log       *log.Logger
	chunkSize int
}

// ReadFrom implements io.ReaderFrom interface.
func (f *flushResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	flusher := http.NewResponseController(f.ResponseWriter) // nolint: bodyclose

	var n int64
	p := make([]byte, f.chunkSize)
	for {
		nr, err := r.Read(p)
		if errors.Is(err, io.EOF) {
			break
		}
		nw, err := f.ResponseWriter.Write(p[:nr])
		if err != nil {
			logf(f.log, "error writing response: %v", err)
			renderStatusError(f.ResponseWriter, http.StatusInternalServerError)
			return n, err
		}
		if nr != nw {
			return n, err
		}
		n += int64(nr)
		// ResponseWriter must support http.Flusher to handle buffered output.
		if err := flusher.Flush(); err != nil {
			logf(f.log, "mismatched bytes written: expected %d, wrote %d", nr, nw)
			renderStatusError(f.ResponseWriter, http.StatusInternalServerError)
			return n, fmt.Errorf("%w: error while flush", err)
		}
	}

	return n, nil
}

// Close implements io.Closer interface.
// It is a no-op.
func (f *flushResponseWriter) Close() error {
	return nil
}

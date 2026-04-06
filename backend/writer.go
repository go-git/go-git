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
			f.log.Printf("error writing response: %v", err)
			renderStatusError(f.ResponseWriter, http.StatusInternalServerError)
			return n, err
		}
		if nr != nw {
			return n, err
		}
		n += int64(nr)
		if err := flusher.Flush(); err != nil {
			f.log.Printf("mismatched bytes written: expected %d, wrote %d", nr, nw)
			renderStatusError(f.ResponseWriter, http.StatusInternalServerError)
			return n, fmt.Errorf("%w: error while flush", err)
		}
	}

	return n, nil
}

// Close implements io.Closer. It is a no-op.
func (f *flushResponseWriter) Close() error {
	return nil
}

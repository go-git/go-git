package idxfile

import "bytes"

// nopCloserReaderAt wraps a bytes.Reader to satisfy ReadAtCloser.
type nopCloserReaderAt struct {
	*bytes.Reader
}

func (nopCloserReaderAt) Close() error { return nil }

// NewBytesReadAtCloser wraps data in a [ReadAtCloser] backed by a
// [*bytes.Reader]. The Close method is a no-op. This is provided for
// callers outside the idxfile package that need to construct an
// in-memory ReadAtCloser (e.g. for rev opener closures in dumb HTTP).
func NewBytesReadAtCloser(data []byte) ReadAtCloser {
	return nopCloserReaderAt{bytes.NewReader(data)}
}

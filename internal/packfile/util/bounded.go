package util

import (
	"errors"
	"io"
)

// ErrInflatedSizeMismatch is returned when a packfile object inflates to
// more bytes than the size declared in its object header. It is
// re-exported as packfile.ErrInflatedSizeMismatch.
var ErrInflatedSizeMismatch = errors.New("packfile: inflated object exceeds declared size")

// BoundedReadCloser wraps a ReadCloser and reports ErrInflatedSizeMismatch
// once more than limit bytes have been read. It is used by the on-demand
// object readers to enforce the same bound that the scanner applies during
// a forward scan, so a lazy Read of a packfile object cannot stream past
// its declared inflated size.
//
// The implementation builds on io.LimitedReader with the standard
// overrun-detection trick: request limit+1 bytes from the underlying so
// that the moment the sentinel byte materializes (LimitedReader.N drops
// to zero) we know the source produced more than limit bytes.
type BoundedReadCloser struct {
	lr      io.LimitedReader
	closer  io.Closer
	overrun bool
}

// NewBoundedReadCloser wraps rc so that the cumulative bytes returned from
// Read never exceed limit. The first call that would have returned a byte
// past limit instead returns ErrInflatedSizeMismatch; subsequent calls
// keep returning the same error. A negative limit is treated as zero, so
// the first byte produced by rc surfaces ErrInflatedSizeMismatch.
func NewBoundedReadCloser(rc io.ReadCloser, limit int64) *BoundedReadCloser {
	if limit < 0 {
		limit = 0
	}
	return &BoundedReadCloser{
		lr:     io.LimitedReader{R: rc, N: limit + 1},
		closer: rc,
	}
}

// Read forwards Read up to the configured byte limit. When the underlying
// stream produces the limit+1 sentinel byte, the legal prefix is returned
// alongside ErrInflatedSizeMismatch; on subsequent calls only the error
// is returned.
func (b *BoundedReadCloser) Read(p []byte) (int, error) {
	if b.overrun {
		return 0, ErrInflatedSizeMismatch
	}
	n, err := b.lr.Read(p)
	if b.lr.N == 0 {
		b.overrun = true
		return n - 1, ErrInflatedSizeMismatch
	}
	return n, err
}

// Close closes the underlying ReadCloser.
func (b *BoundedReadCloser) Close() error { return b.closer.Close() }

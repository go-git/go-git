package transport

import (
	"context"
	"errors"
)

// ReadFinished reports whether the reader wrapped by ioutil.NewContextReader
// or ioutil.NewContextReadCloser has finished with the reader underneath it,
// so that reader (typically a network connection) may be closed after a read
// returned err.
//
// It has not when this context's cancellation is what ended the read. The
// wrapper returns from its <-ctx.Done() branch while the goroutine it started
// can still be blocked in the underlying Read, and closing would race that
// goroutine. Nothing is leaked by leaving it, because a reader bound to the
// same context is torn down by that context anyway. Any other outcome, success
// included, means the last Read arrived over the result channel and the
// goroutine is finished, so the underlying reader must be closed or it and its
// connection are held for the life of the process.
//
// A cancellation error alone is not enough to answer no. A deadline belonging
// to something else, such as a storer or a wrapped inner operation, can
// surface as DeadlineExceeded while this context is still live, and treating
// that as unsafe leaks the reader. A cancelled context alone is not enough
// either. It can be cancelled an instant after a read that had already
// finished, and skipping the close there leaks it too.
//
// The two together are not exact. Once this context is done, a cancellation
// that came from somewhere else reads the same as its own, and the close is
// skipped for a read that had in fact finished. The reader that answer is
// wrong about is a reader bound to a context that is already done, which its
// own teardown closes. A reader bound to something else is leaked.
func ReadFinished(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return true
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

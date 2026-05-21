package packhandle

import (
	"bytes"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v6/internal/sharedfile"
)

// memCloser wraps a *bytes.Reader to satisfy ReadAtCloser.
// Used by cursor_reader_test.go and pack_meta_test.go.
type memCloser struct {
	*bytes.Reader
	closed atomic.Bool
}

func (m *memCloser) Close() error {
	m.closed.Store(true)
	return nil
}

// newSharedFile is a convenience wrapper around [sharedfile.New]
// for use in packhandle tests.
func newSharedFile(open func() (ReadAtCloser, error), gracePeriod time.Duration) *sharedfile.SharedFile {
	return sharedfile.New(open, gracePeriod)
}

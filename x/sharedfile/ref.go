package sharedfile

import "sync/atomic"

// Ref is a durable, single-owner reference to a SharedFile's open handle.
// It holds one reference from Ref() until Close(). While any Ref (or cursor)
// is live the underlying fd/mapping cannot be torn down by the grace timer,
// ReleaseNow, Retire, or fdpool eviction. Callers read through File() for the
// Ref's whole lifetime; they must not re-Acquire the SharedFile per read.
type Ref struct {
	sf     *SharedFile
	file   ReadAtCloser
	closed atomic.Bool
}

// Ref acquires one reference and wraps it. Returns ErrClosed if the SharedFile
// has been Closed or Retired.
func (s *SharedFile) Ref() (*Ref, error) {
	f, err := s.Acquire()
	if err != nil {
		return nil, err
	}
	return &Ref{sf: s, file: f}, nil
}

// File returns the acquired handle. It may additionally satisfy billy.Mmap
// (Bytes/Slice) when the SharedFile is mmap-backed; the reference this Ref
// holds is what keeps returned slices valid.
func (r *Ref) File() ReadAtCloser { return r.file }

// Close releases the reference. Idempotent.
func (r *Ref) Close() error {
	if r.closed.Swap(true) {
		return nil
	}
	r.sf.Release()
	return nil
}

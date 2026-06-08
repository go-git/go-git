package packfile

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing"
)

// PackHandle is the handle [NewPackfile] consumes when
// [WithPackHandle] is supplied.
type PackHandle interface {
	// OpenPackReader returns a fresh sequential cursor over the
	// .pack file. The cursor is closed by the caller.
	OpenPackReader() (io.ReadSeekCloser, error)
	// OpenRandomReader returns a fresh random-access cursor over
	// the .pack file. The cursor is closed by the caller.
	OpenRandomReader() (RandomReader, error)
	// PackHash returns the .pack file's trailing checksum, which
	// by canonical-Git construction equals the pack's identity
	// hash (the hex in pack-<hash>.pack).
	PackHash() (plumbing.Hash, error)
}

// RandomReader is the per-read random-access cursor returned by
// [PackHandle.OpenRandomReader]. ReadAt is safe to call
// concurrently with itself; Close releases the cursor's hold on
// the underlying pack file descriptor.
type RandomReader interface {
	io.ReaderAt
	io.Closer
}

// PackHandleResolver returns the current [PackHandle] for one
// .pack file. It is invoked on scanner init (once per [Packfile])
// and on every [FSObject.Reader] call. See [DotGit.PackHandle]
// for the reference implementation.
//
// Contract:
//
//   - Every handle returned for the lifetime of a given [Packfile]
//     MUST address the same .pack file on disk (same PackHash).
//     The handle value MAY change across calls. [Packfile] does
//     NOT re-validate identity on re-resolution.
//   - Errors propagate to the caller as object-read errors. The
//     resolver SHOULD NOT retry internally.
//   - The handle returned MUST remain valid until at least one
//     cursor obtained from it has been closed by the caller.
type PackHandleResolver func() (PackHandle, error)

// WithPackHandle injects an externally-owned [PackHandle] resolver.
// The resolved handle is not closed by [Packfile.Close]; its
// lifetime is owned by the resolver. See [PackHandleResolver] for
// the resolver contract.
func WithPackHandle(get PackHandleResolver) PackfileOption {
	return func(p *Packfile) {
		p.resolveHandle = get
	}
}

// openRandomReader re-resolves the pack handle via resolveHandle
// and returns a fresh random-access cursor.
func (p *Packfile) openRandomReader() (RandomReader, error) {
	h, err := p.resolveHandle()
	if err != nil {
		return nil, err
	}
	return h.OpenRandomReader()
}

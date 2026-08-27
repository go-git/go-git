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
	// PackHash returns the .pack file's trailing checksum. It identifies the
	// logical pack and supplies the hash in pack- or loose-named aliases.
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

// PackHandleResolver returns a [PackHandle] for one logical pack identity. It
// runs during scanner initialization and for each [FSObject.Reader] call.
//
// Every returned handle must report the same PackHash. The physical alias can
// change between calls. Storage mutation can invalidate a handle and its open
// cursors; later cursor operations then return an error. Resolver errors
// propagate as object-read errors.
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

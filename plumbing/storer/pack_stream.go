package storer

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
)

// PackStreamOptions configures a PackStreamer call. All fields are hints;
// implementations may ignore any that do not apply.
type PackStreamOptions struct {
	// ThinPack: client supports thin-pack; entries may REF_DELTA against
	// objects the client is already known to have (the haves set is
	// reachable on the client side).
	ThinPack bool

	// SkipDeltaCompression: emit every entry as a full object, no deltas.
	SkipDeltaCompression bool

	// PackWindow: sliding window for delta selection. Zero means
	// "use the implementation's default".
	PackWindow uint

	// ObjectFormat: SHA1 or SHA256. Drives pack header version and trailer.
	ObjectFormat config.ObjectFormat

	// Shallow: hashes the caller has already declared as shallow boundaries
	// (after any deepen / deepen-since / deepen-not processing). Empty for
	// non-shallow requests.
	Shallow []plumbing.Hash

	// Progress, when non-nil, is the sideband progress band (band 2). The
	// PackStreamer owns all progress emission on this writer when it is
	// supplied; the caller will not write any progress of its own.
	Progress io.Writer
}

// PackStreamer is an optional interface a Storer can implement to take
// over upload-pack pack emission. When implemented, the caller writes the
// protocol envelope (request decode, ACK negotiation, sideband mux,
// shallow update) and delegates the entire pack body (header, entries,
// trailer) to StreamPack.
//
// The writer w is whatever the caller has already wrapped — the data
// band of a sideband mux when sideband is negotiated, the raw writer
// otherwise. Implementations write pack bytes only.
type PackStreamer interface {
	StreamPack(ctx context.Context, w io.Writer, wants, haves []plumbing.Hash, opts PackStreamOptions) error
}

// PackObjectWalker is an optional interface a Storer can implement to
// produce the object set for a pack from wants/haves without going
// through the generic revlist walk. Backends with reachability bitmaps
// can satisfy this much faster than the generic walker.
type PackObjectWalker interface {
	PackObjects(ctx context.Context, wants, haves []plumbing.Hash) ([]plumbing.Hash, error)
}

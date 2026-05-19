package packhandle

import (
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

// defaultGracePeriod is the idle window before a sharedFile's
// AfterFunc closes the underlying FD. One second balances FD reuse
// against worst-case FD count under bursty workloads.
const defaultGracePeriod = 1 * time.Second

// PackMeta is the parsed pack header (Version, Count) and footer
// hash (ID). The parser implementation arrives in the same commit
// that adds Meta().
type PackMeta struct {
	Version uint32
	Count   uint32
	ID      plumbing.Hash
}

// PackHandle owns FD lifecycle for one pack's files and serves as
// the single facade for pack access.
type PackHandle struct {
	sources  Sources
	packHash plumbing.Hash
	pack     *sharedFile

	// closed is set true at the start of Close (before any FDs are
	// released). Index/Meta/OpenPackReader/OpenRandomReader check
	// this and return fs.ErrClosed without doing work, preventing
	// the half-closed retry race where a goroutine retries Index()
	// after a transient failure and re-opens idx/rev FDs after
	// Close has already torn down the pack sharedFile.
	closed atomic.Bool

	// metaMu guards metaVal. metaVal nil → not yet built (or last
	// attempt failed). Successful reads are cached; failures are
	// not (next call retries). Meta() and Index() implementations
	// arrive in subsequent commits.
	metaMu  sync.Mutex
	metaVal *PackMeta

	// indexMu guards indexVal. Same nil-pointer-as-not-built
	// signal. indexVal is the concrete *idxfile.LazyIndex (not
	// the idxfile.Index interface) so Close can call its Close()
	// directly — idxfile.Index has no Close method. When the
	// future idxfile.Index redesign lands, this field's type
	// swaps; the call sites affected are all inside this package.
	indexMu  sync.Mutex
	indexVal *idxfile.LazyIndex

	closeFn func() error // sync.OnceValue — idempotent Close
}

// New constructs a PackHandle. packHash is pinned at construction
// so Meta() can verify the footer and the hash size is unambiguous
// for the lifetime of the handle.
//
// Returns ErrPackSourceRequired if Sources.Pack.Open or
// Sources.Pack.Size is nil. Returns ErrInvalidPackHash if packHash
// is the zero hash. Sources.Idx/.Rev are optional; Index() returns
// ErrSourceUnconfigured when either is zero.
func New(sources Sources, packHash plumbing.Hash) (*PackHandle, error) {
	if sources.Pack.Open == nil || sources.Pack.Size == nil {
		return nil, ErrPackSourceRequired
	}
	if packHash.IsZero() {
		return nil, ErrInvalidPackHash
	}
	h := &PackHandle{
		sources:  sources,
		packHash: packHash,
		pack:     newSharedFile(sources.Pack.Open, defaultGracePeriod),
	}
	h.closeFn = sync.OnceValue(h.doClose)
	return h, nil
}

// OpenPackReader hands out a fresh streaming cursor over the .pack
// file. Each call returns an independent cursor.
func (h *PackHandle) OpenPackReader() (PackReader, error) {
	if h.closed.Load() {
		return nil, fs.ErrClosed
	}
	size, err := h.sources.Pack.Size()
	if err != nil {
		return nil, fmt.Errorf("packhandle: pack size: %w", err)
	}
	return newCursorReader(h.pack, size)
}

// OpenRandomReader hands out a fresh random-access cursor over the
// .pack file. Each call returns an independent cursor.
func (h *PackHandle) OpenRandomReader() (RandomReader, error) {
	if h.closed.Load() {
		return nil, fs.ErrClosed
	}
	size, err := h.sources.Pack.Size()
	if err != nil {
		return nil, fmt.Errorf("packhandle: pack size: %w", err)
	}
	return newCursorReader(h.pack, size)
}

// Close releases the .pack sharedFile and closes the cached
// LazyIndex (if one was successfully built). Idempotent.
func (h *PackHandle) Close() error {
	return h.closeFn()
}

func (h *PackHandle) doClose() error {
	// Set closed BEFORE touching any FDs so any concurrent retry-
	// after-transient-failure path in Index/Meta sees the flag and
	// bails with fs.ErrClosed rather than re-opening idx/rev FDs
	// against a torn-down pack.
	h.closed.Store(true)

	packErr := h.pack.Close()

	h.indexMu.Lock()
	idx := h.indexVal
	h.indexVal = nil
	h.indexMu.Unlock()

	var idxErr error
	if idx != nil {
		idxErr = idx.Close()
	}

	if packErr == nil && idxErr == nil {
		return nil
	}
	if packErr == nil {
		return idxErr
	}
	if idxErr == nil {
		return packErr
	}
	return fmt.Errorf("packhandle: close: pack=%v idx=%v", packErr, idxErr)
}

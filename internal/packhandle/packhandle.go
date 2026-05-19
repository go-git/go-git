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

// defaultGracePeriod is the idle window after the last cursor
// release before the .pack file descriptor is closed.
const defaultGracePeriod = 1 * time.Second

// PackHandle reads from one pack triple, owning the .pack file
// descriptor for its lifetime and constructing an [idxfile.Index]
// over the .idx/.rev pair on demand.
//
// The .pack file descriptor is opened lazily on first cursor
// request, shared across concurrent readers, and closed after an
// idle grace period. .idx and .rev descriptors are owned by the
// returned [idxfile.Index].
//
// All methods are safe for concurrent use.
type PackHandle struct {
	sources  Sources
	packHash plumbing.Hash
	pack     *sharedFile

	closed atomic.Bool

	metaMu  sync.Mutex
	metaVal *PackMeta

	indexMu  sync.Mutex
	indexVal *idxfile.LazyIndex

	sizeVal atomic.Int64

	closeFn func() error
}

// New constructs a [PackHandle] over the given sources. packHash
// is pinned to the .pack file's expected footer hash; [PackHandle.Meta]
// verifies the footer against this value.
//
// Returns [ErrPackSourceRequired] if Sources.Pack.Open or
// Sources.Pack.Size is nil, and [ErrInvalidPackHash] if packHash
// is the zero hash. Sources.Idx and Sources.Rev are optional;
// [PackHandle.Index] returns [ErrSourceUnconfigured] when either
// is absent.
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

// OpenPackReader returns a streaming cursor over the .pack file.
// Each call returns an independent cursor with its own offset.
func (h *PackHandle) OpenPackReader() (PackReader, error) {
	if h.closed.Load() {
		return nil, fs.ErrClosed
	}
	size, err := h.packSize()
	if err != nil {
		return nil, fmt.Errorf("packhandle: pack size: %w", err)
	}
	return newCursorReader(h.pack, size)
}

// OpenRandomReader returns a random-access cursor over the .pack
// file. Each call returns an independent cursor.
func (h *PackHandle) OpenRandomReader() (RandomReader, error) {
	if h.closed.Load() {
		return nil, fs.ErrClosed
	}
	size, err := h.packSize()
	if err != nil {
		return nil, fmt.Errorf("packhandle: pack size: %w", err)
	}
	return newCursorReader(h.pack, size)
}

// packSize returns the cached .pack file size, consulting
// Sources.Pack.Size only on the first call. The .pack file is
// immutable post-creation and its on-disk identity is pinned via
// packHash, so the size is invariant for the lifetime of this
// handle. Failures are not cached; the next call retries.
func (h *PackHandle) packSize() (int64, error) {
	if v := h.sizeVal.Load(); v != 0 {
		return v, nil
	}
	size, err := h.sources.Pack.Size()
	if err != nil {
		return 0, err
	}
	h.sizeVal.Store(size)
	return size, nil
}

// Close releases the .pack [sharedFile] and closes any cached
// index. Idempotent.
func (h *PackHandle) Close() error {
	return h.closeFn()
}

func (h *PackHandle) doClose() error {
	// Set closed before releasing any FDs so a concurrent retry in
	// Index or Meta sees the flag and bails with fs.ErrClosed
	// instead of reopening idx/rev FDs against a torn-down pack.
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

// Meta reads and verifies the .pack header and footer hash. The
// first successful call is cached; transient open or read
// failures retry on the next call. Returns [fs.ErrClosed] if the
// [PackHandle] is closed.
func (h *PackHandle) Meta() (PackMeta, error) {
	if h.closed.Load() {
		return PackMeta{}, fs.ErrClosed
	}
	h.metaMu.Lock()
	defer h.metaMu.Unlock()
	if h.closed.Load() {
		return PackMeta{}, fs.ErrClosed
	}
	if h.metaVal != nil {
		return *h.metaVal, nil
	}

	size, err := h.packSize()
	if err != nil {
		return PackMeta{}, fmt.Errorf("packhandle: pack size: %w", err)
	}
	src, err := h.pack.Acquire()
	if err != nil {
		return PackMeta{}, fmt.Errorf("packhandle: acquire pack: %w", err)
	}
	defer h.pack.Release()

	meta, err := parsePackMeta(src, size, h.packHash)
	if err != nil {
		return PackMeta{}, err
	}
	h.metaVal = &meta
	return meta, nil
}

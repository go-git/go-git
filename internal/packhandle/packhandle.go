package packhandle

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v6/internal/sharedfile"
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
// Lifecycle contract:
//
//   - Each cursor returned by [PackHandle.OpenPackReader] or
//     [PackHandle.OpenRandomReader] acquires one reference on the
//     underlying [sharedfile.SharedFile]; cursor.Close releases
//     it. While at least one cursor is live the .pack FD cannot
//     be torn down by the grace timer.
//   - [PackHandle.Close] is synchronous: the .pack FD and any
//     cached [idxfile.LazyIndex] FDs are closed before the call
//     returns. Cursors opened before Close keep working until
//     their own Close releases the last reference; calls on a
//     cursor whose underlying FD has been closed see
//     [fs.ErrClosed].
//
// All methods are safe for concurrent use.
type PackHandle struct {
	sources  Sources
	packHash plumbing.Hash
	pack     *sharedfile.SharedFile

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
		pack:     sharedfile.New(sources.Pack.Open, defaultGracePeriod),
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
//
// The cache uses an [atomic.Int64] with zero as the unset
// sentinel. Pack sizes are never zero — every valid pack carries
// at least a 12-byte header and a footer hash — so a zero load
// unambiguously means "not yet cached." If that invariant ever
// changes, this loop will re-Size on every call and the cache
// becomes dead code.
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

// Close releases the .pack [sharedfile.SharedFile] and closes any
// cached index. Idempotent.
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

	return errors.Join(packErr, idxErr)
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

// CloseIdleDescriptors releases the .pack file descriptor and
// the idx/rev descriptors of any cached [idxfile.LazyIndex]
// without marking the [PackHandle] closed. Active acquired
// readers continue to work; FDs held by in-flight readers close
// the instant the last refcount drops to zero. Subsequent
// [PackHandle.OpenPackReader] and [PackHandle.Index] operations
// reopen FDs on demand and resume normal grace-timer behaviour.
//
// Idempotent and safe to call concurrently with the open paths
// and itself. A no-op after [PackHandle.Close]; the closed flag
// short-circuits before touching either [sharedfile.SharedFile].
//
// PackHandle-level caches survive: the cached [PackMeta] and
// the cached [idxfile.LazyIndex] pointer are not reset. A
// caller that wants to discard the PackHandle entirely uses
// Close; a subsequent Close after CloseIdleDescriptors still
// flips each underlying [sharedfile.SharedFile]'s closed flag
// exactly once via its idempotent Close.
func (h *PackHandle) CloseIdleDescriptors() error {
	if h.closed.Load() {
		return nil
	}

	packErr := h.pack.ReleaseNow()

	h.indexMu.Lock()
	idx := h.indexVal
	h.indexMu.Unlock()

	var idxErr error
	if idx != nil {
		idxErr = idx.CloseIdleDescriptors()
	}
	return errors.Join(packErr, idxErr)
}

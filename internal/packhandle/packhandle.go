package packhandle

import (
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v6/internal/sharedfile"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/x/fdpool"
)

// defaultGracePeriod is the idle window after the last cursor
// release before the .pack file descriptor is closed.
const defaultGracePeriod = 1 * time.Second

// PackHandle reads one logical pack.
//
// The pack descriptor opens lazily. Idle release follows the configured pool
// or grace-period policy. Close is permanent. It invalidates active cursors,
// and their last Close releases the descriptor. Later cursor operations return
// [fs.ErrClosed].
//
// PackHandle methods are safe for concurrent use. Read and Seek are not safe to
// call concurrently on one streaming cursor.
type PackHandle struct {
	source   Source
	packHash plumbing.Hash
	pack     *sharedfile.SharedFile

	closed atomic.Bool

	hashMu    sync.Mutex
	hashValid bool

	sizeVal atomic.Int64

	closeFn func() error
}

// NewWithPool constructs a PackHandle and registers its shared pack descriptor
// with pool. Positive capacity keeps the descriptor registered until eviction
// or Close. A nil or nonpositive-capacity pool uses the grace timer.
//
// Returns [ErrPackSourceRequired] if Open or Size is nil, and
// [ErrInvalidPackHash] if packHash is zero.
func NewWithPool(source Source, packHash plumbing.Hash, pool *fdpool.Pool) (*PackHandle, error) {
	if source.Open == nil || source.Size == nil {
		return nil, ErrPackSourceRequired
	}
	if packHash.IsZero() {
		return nil, ErrInvalidPackHash
	}
	h := &PackHandle{
		source:   source,
		packHash: packHash,
		pack:     sharedfile.NewWithPool(source.Open, defaultGracePeriod, pool),
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
// Source.Size only on the first call. The .pack file is
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
	size, err := h.source.Size()
	if err != nil {
		return 0, err
	}
	h.sizeVal.Store(size)
	return size, nil
}

// Close permanently releases the shared pack file. It is idempotent.
func (h *PackHandle) Close() error {
	return h.closeFn()
}

func (h *PackHandle) doClose() error {
	// Set closed before release so a concurrent PackHash retry cannot reopen the
	// descriptor after terminal close.
	h.closed.Store(true)
	return h.pack.Close()
}

// PackHash validates the pack footer against the pinned hash. The first
// successful validation is cached. A failed validation retries on the next
// call. PackHash returns [fs.ErrClosed] after Close.
func (h *PackHandle) PackHash() (plumbing.Hash, error) {
	if h.closed.Load() {
		return plumbing.ZeroHash, fs.ErrClosed
	}
	h.hashMu.Lock()
	defer h.hashMu.Unlock()
	if h.closed.Load() {
		return plumbing.ZeroHash, fs.ErrClosed
	}
	if h.hashValid {
		return h.packHash, nil
	}

	size, err := h.packSize()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("packhandle: pack size: %w", err)
	}
	src, err := h.pack.Acquire()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("packhandle: acquire pack: %w", err)
	}
	defer h.pack.Release()

	if err := validatePackHash(src, size, h.packHash); err != nil {
		return plumbing.ZeroHash, err
	}
	h.hashValid = true
	return h.packHash, nil
}

// CloseIdleDescriptors requests release of the idle pack descriptor without
// closing the PackHandle or clearing its caches. Active readers retain the
// descriptor until they finish. Later operations reopen it.
func (h *PackHandle) CloseIdleDescriptors() error {
	if h.closed.Load() {
		return nil
	}

	return h.pack.ReleaseNow()
}

package packhandle

import (
	"io/fs"

	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

// Index returns a lazily-constructed [idxfile.Index] backed by
// the Idx and Rev sources. The first successful build is cached;
// transient build failures are not cached and retry on the next
// call. Returns [ErrSourceUnconfigured] if Idx or Rev was not
// configured, or [fs.ErrClosed] if the [PackHandle] is closed.
func (h *PackHandle) Index() (idxfile.Index, error) {
	if h.closed.Load() {
		return nil, fs.ErrClosed
	}
	h.indexMu.Lock()
	defer h.indexMu.Unlock()
	if h.closed.Load() {
		return nil, fs.ErrClosed
	}
	if h.indexVal != nil {
		return h.indexVal, nil
	}
	if h.sources.Idx.Open == nil || h.sources.Rev.Open == nil {
		return nil, ErrSourceUnconfigured
	}

	idxOpener := func() (idxfile.ReadAtCloser, error) { return h.sources.Idx.Open() }
	revOpener := func() (idxfile.ReadAtCloser, error) { return h.sources.Rev.Open() }

	lazy, err := idxfile.NewLazyIndex(idxOpener, revOpener, h.packHash)
	if err != nil {
		return nil, err
	}
	h.indexVal = lazy
	return lazy, nil
}

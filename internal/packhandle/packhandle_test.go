package packhandle_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
)

// validSourcesFromFixture wires Sources against the basic fixture's
// pack triple, materialized on a real osfs.New(t.TempDir()).
//
// Each of fixture.Packfile(), fixture.Idx(), and fixture.Rev()
// writes the embedded fixture file to a fresh OS temp path under dir
// and returns an open billy.File. We close each returned handle and
// re-open via PathSource so that PathSource owns its own FDs.
//
// Returns (Sources, packHash) for use with packhandle.New.
func validSourcesFromFixture(t *testing.T) (packhandle.Sources, plumbing.Hash) {
	t.Helper()
	dir := t.TempDir()
	fixture := fixtures.NewOSFixture(fixtures.Basic().One(), dir)

	packFile, err := fixture.Packfile()
	if err != nil {
		t.Fatalf("fixture.Packfile: %v", err)
	}
	packPath := packFile.Name()
	_ = packFile.Close()

	idxFile, err := fixture.Idx()
	if err != nil {
		t.Fatalf("fixture.Idx: %v", err)
	}
	idxPath := idxFile.Name()
	_ = idxFile.Close()

	revFile, err := fixture.Rev()
	if err != nil {
		t.Fatalf("fixture.Rev: %v", err)
	}
	revPath := revFile.Name()
	_ = revFile.Close()

	bfs := osfs.New(dir)
	srcs := packhandle.Sources{
		Pack: packhandle.PathSource(bfs, packPath),
		Idx:  packhandle.PathSource(bfs, idxPath),
		Rev:  packhandle.PathSource(bfs, revPath),
	}
	hash := plumbing.NewHash(fixture.PackfileHash)
	if hash.IsZero() {
		t.Fatalf("fixture.PackfileHash %q yields zero hash", fixture.PackfileHash)
	}
	return srcs, hash
}

func TestNew_ReturnsErrorOnNilPackOpen(t *testing.T) {
	t.Parallel()
	srcs := packhandle.Sources{
		Pack: packhandle.Source{
			Open: nil,
			Size: func() (int64, error) { return 0, nil },
		},
	}
	_, err := packhandle.New(srcs, plumbing.NewHash("ffff"))
	if !errors.Is(err, packhandle.ErrPackSourceRequired) {
		t.Fatalf("err = %v, want ErrPackSourceRequired", err)
	}
}

func TestNew_ReturnsErrorOnNilPackSize(t *testing.T) {
	t.Parallel()
	srcs := packhandle.Sources{
		Pack: packhandle.Source{
			Open: func() (packhandle.ReadAtCloser, error) { return nil, nil },
			Size: nil,
		},
	}
	_, err := packhandle.New(srcs, plumbing.NewHash("ffff"))
	if !errors.Is(err, packhandle.ErrPackSourceRequired) {
		t.Fatalf("err = %v, want ErrPackSourceRequired", err)
	}
}

func TestNew_ReturnsErrorOnZeroHash(t *testing.T) {
	t.Parallel()
	srcs, _ := validSourcesFromFixture(t)
	_, err := packhandle.New(srcs, plumbing.ZeroHash)
	if !errors.Is(err, packhandle.ErrInvalidPackHash) {
		t.Fatalf("err = %v, want ErrInvalidPackHash", err)
	}
}

func TestNew_AcceptsZeroIdxAndRev(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	srcs.Idx = packhandle.Source{}
	srcs.Rev = packhandle.Source{}
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()
}

func TestOpenPackReader_ReadsFirstFourBytes(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	r, err := h.OpenPackReader()
	if err != nil {
		t.Fatalf("OpenPackReader: %v", err)
	}
	defer r.Close()

	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if !bytes.Equal(buf, []byte("PACK")) {
		t.Fatalf("first 4 bytes = %q, want \"PACK\"", buf)
	}
}

func TestOpenRandomReader_ReadAtAnyOffset(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	r, err := h.OpenRandomReader()
	if err != nil {
		t.Fatalf("OpenRandomReader: %v", err)
	}
	defer r.Close()

	buf := make([]byte, 4)
	if _, err := r.ReadAt(buf, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(buf, []byte("PACK")) {
		t.Fatalf("ReadAt at 0 = %q, want \"PACK\"", buf)
	}
}

func TestClose_IsIdempotent(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestOpenPackReader_AfterCloseReturnsErrClosed(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := h.OpenPackReader(); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("OpenPackReader after Close: %v, want fs.ErrClosed", err)
	}
	if _, err := h.OpenRandomReader(); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("OpenRandomReader after Close: %v, want fs.ErrClosed", err)
	}
}

func TestMeta_HappyPath(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	meta, err := h.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.Version != 2 {
		t.Fatalf("Version = %d, want 2", meta.Version)
	}
	if meta.Count == 0 {
		t.Fatalf("Count = 0, want > 0")
	}
	if meta.ID != hash {
		t.Fatalf("ID = %v, want %v", meta.ID, hash)
	}
}

func TestMeta_CachedAcrossCalls(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	first, err := h.Meta()
	if err != nil {
		t.Fatalf("first Meta: %v", err)
	}
	second, err := h.Meta()
	if err != nil {
		t.Fatalf("second Meta: %v", err)
	}
	if first != second {
		t.Fatalf("Meta values differ across calls: %v vs %v", first, second)
	}
}

func TestMeta_HashMismatchSurfacesError(t *testing.T) {
	t.Parallel()
	srcs, _ := validSourcesFromFixture(t)
	wrongHash := plumbing.NewHash("0000000000000000000000000000000000000001")
	h, err := packhandle.New(srcs, wrongHash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if _, err := h.Meta(); err == nil {
		t.Fatalf("Meta returned no error against wrong packHash")
	}
}

func TestMeta_AfterCloseReturnsErrClosed(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)
	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := h.Meta(); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Meta after Close: %v, want fs.ErrClosed", err)
	}
}

// TestPackSize_CachedAcrossCallSites confirms that the cached
// .pack file size is consulted from a single Sources.Pack.Size()
// invocation regardless of how many cursor opens or Meta calls
// follow. Pack files are immutable post-creation, so the cache
// avoids a per-cursor-open fs.Stat on the hot path.
func TestPackSize_CachedAcrossCallSites(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)

	var sizeCalls atomic.Int32
	origSize := srcs.Pack.Size
	srcs.Pack.Size = func() (int64, error) {
		sizeCalls.Add(1)
		return origSize()
	}

	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	r1, err := h.OpenPackReader()
	if err != nil {
		t.Fatalf("OpenPackReader: %v", err)
	}
	_ = r1.Close()

	r2, err := h.OpenRandomReader()
	if err != nil {
		t.Fatalf("OpenRandomReader: %v", err)
	}
	_ = r2.Close()

	if _, err := h.Meta(); err != nil {
		t.Fatalf("Meta: %v", err)
	}

	if got := sizeCalls.Load(); got != 1 {
		t.Fatalf("Pack.Size called %d times across OpenPackReader, OpenRandomReader, Meta; want 1", got)
	}
}

// TestPackSize_FailureNotCached confirms that a transient
// Sources.Pack.Size() failure is not cached: the next call
// retries and on success populates the cache.
func TestPackSize_FailureNotCached(t *testing.T) {
	t.Parallel()
	srcs, hash := validSourcesFromFixture(t)

	var calls atomic.Int32
	var failNext atomic.Bool
	failNext.Store(true)
	origSize := srcs.Pack.Size
	srcs.Pack.Size = func() (int64, error) {
		calls.Add(1)
		if failNext.CompareAndSwap(true, false) {
			return 0, errors.New("transient stat failure")
		}
		return origSize()
	}

	h, err := packhandle.New(srcs, hash)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if _, err := h.OpenPackReader(); err == nil {
		t.Fatalf("first OpenPackReader: expected error, got nil")
	}

	r, err := h.OpenPackReader()
	if err != nil {
		t.Fatalf("second OpenPackReader: %v", err)
	}
	_ = r.Close()

	if _, err := h.OpenRandomReader(); err != nil {
		t.Fatalf("third call (OpenRandomReader): %v", err)
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("Pack.Size called %d times; want 2 (first fails, second succeeds and caches; third hits cache)", got)
	}
}

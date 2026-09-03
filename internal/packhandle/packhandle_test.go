package packhandle_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
)

func pathSource(fs billy.Basic, path string) packhandle.Source {
	return packhandle.Source{
		Open: func() (packhandle.ReadAtCloser, error) { return fs.Open(path) },
		Size: func() (int64, error) {
			info, err := fs.Stat(path)
			if err != nil {
				return 0, err
			}
			return info.Size(), nil
		},
	}
}

// validSourceFromFixture returns a source for one materialized fixture pack.
func validSourceFromFixture(t *testing.T) (packhandle.Source, plumbing.Hash) {
	t.Helper()
	dir := t.TempDir()
	fixture := fixtures.NewOSFixture(fixtures.Basic().One(), dir)

	packFile, err := fixture.Packfile()
	if err != nil {
		t.Fatalf("fixture.Packfile: %v", err)
	}
	packPath := packFile.Name()
	_ = packFile.Close()

	bfs := osfs.New(dir)
	src := pathSource(bfs, packPath)
	hash := plumbing.NewHash(fixture.PackfileHash)
	if hash.IsZero() {
		t.Fatalf("fixture.PackfileHash %q yields zero hash", fixture.PackfileHash)
	}
	return src, hash
}

func TestNewWithPool_ReturnsErrorOnNilPackOpen(t *testing.T) {
	t.Parallel()
	src := packhandle.Source{
		Open: nil,
		Size: func() (int64, error) { return 0, nil },
	}
	_, err := packhandle.NewWithPool(src, plumbing.NewHash("ffff"), nil)
	if !errors.Is(err, packhandle.ErrPackSourceRequired) {
		t.Fatalf("err = %v, want ErrPackSourceRequired", err)
	}
}

func TestNewWithPool_ReturnsErrorOnNilPackSize(t *testing.T) {
	t.Parallel()
	src := packhandle.Source{
		Open: func() (packhandle.ReadAtCloser, error) { return nil, nil },
		Size: nil,
	}
	_, err := packhandle.NewWithPool(src, plumbing.NewHash("ffff"), nil)
	if !errors.Is(err, packhandle.ErrPackSourceRequired) {
		t.Fatalf("err = %v, want ErrPackSourceRequired", err)
	}
}

func TestNewWithPool_ReturnsErrorOnZeroHash(t *testing.T) {
	t.Parallel()
	src, _ := validSourceFromFixture(t)
	_, err := packhandle.NewWithPool(src, plumbing.ZeroHash, nil)
	if !errors.Is(err, packhandle.ErrInvalidPackHash) {
		t.Fatalf("err = %v, want ErrInvalidPackHash", err)
	}
}

func TestOpenPackReader_ReadsFirstFourBytes(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)
	h, err := packhandle.NewWithPool(src, hash, nil)
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
	src, hash := validSourceFromFixture(t)
	h, err := packhandle.NewWithPool(src, hash, nil)
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
	src, hash := validSourceFromFixture(t)
	h, err := packhandle.NewWithPool(src, hash, nil)
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
	src, hash := validSourceFromFixture(t)
	h, err := packhandle.NewWithPool(src, hash, nil)
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

func TestPackHash_HappyPath(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)
	h, err := packhandle.NewWithPool(src, hash, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	got, err := h.PackHash()
	if err != nil {
		t.Fatalf("PackHash: %v", err)
	}
	if got != hash {
		t.Fatalf("PackHash = %v, want %v", got, hash)
	}
}

type countingReadAtCloser struct {
	packhandle.ReadAtCloser
	reads *atomic.Int32
}

func (r *countingReadAtCloser) ReadAt(p []byte, off int64) (int, error) {
	r.reads.Add(1)
	return r.ReadAtCloser.ReadAt(p, off)
}

func TestPackHash_CachedAcrossCalls(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)
	open := src.Open
	var reads atomic.Int32
	src.Open = func() (packhandle.ReadAtCloser, error) {
		f, err := open()
		if err != nil {
			return nil, err
		}
		return &countingReadAtCloser{ReadAtCloser: f, reads: &reads}, nil
	}
	h, err := packhandle.NewWithPool(src, hash, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	first, err := h.PackHash()
	if err != nil {
		t.Fatalf("first PackHash: %v", err)
	}
	firstReads := reads.Load()
	second, err := h.PackHash()
	if err != nil {
		t.Fatalf("second PackHash: %v", err)
	}
	if first != second {
		t.Fatalf("PackHash values differ across calls: %v vs %v", first, second)
	}
	if reads.Load() != firstReads {
		t.Fatalf("cached PackHash performed another ReadAt")
	}
}

func TestPackHash_HashMismatchSurfacesError(t *testing.T) {
	t.Parallel()
	src, _ := validSourceFromFixture(t)
	wrongHash := plumbing.NewHash("0000000000000000000000000000000000000001")
	h, err := packhandle.NewWithPool(src, wrongHash, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if _, err := h.PackHash(); err == nil {
		t.Fatalf("PackHash returned no error against wrong packHash")
	}
}

func TestPackHash_AfterCloseReturnsErrClosed(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)
	h, err := packhandle.NewWithPool(src, hash, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := h.PackHash(); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("PackHash after Close: %v, want fs.ErrClosed", err)
	}
}

// TestPackSize_CachedAcrossCallSites confirms that one successful Source.Size
// call serves cursor opens and PackHash validation.
func TestPackSize_CachedAcrossCallSites(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)

	var sizeCalls atomic.Int32
	origSize := src.Size
	src.Size = func() (int64, error) {
		sizeCalls.Add(1)
		return origSize()
	}

	h, err := packhandle.NewWithPool(src, hash, nil)
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

	if _, err := h.PackHash(); err != nil {
		t.Fatalf("PackHash: %v", err)
	}

	if got := sizeCalls.Load(); got != 1 {
		t.Fatalf("Source.Size called %d times; want 1", got)
	}
}

// TestPackSize_FailureNotCached confirms that a transient Source.Size failure
// retries and that the first success populates the cache.
func TestPackSize_FailureNotCached(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)

	var calls atomic.Int32
	var failNext atomic.Bool
	failNext.Store(true)
	origSize := src.Size
	src.Size = func() (int64, error) {
		calls.Add(1)
		if failNext.CompareAndSwap(true, false) {
			return 0, errors.New("transient stat failure")
		}
		return origSize()
	}

	h, err := packhandle.NewWithPool(src, hash, nil)
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

// countingOpen wraps a Source.Open with an open-counter so tests
// can assert reopen behaviour after CloseIdleDescriptors.
func countingOpen(src packhandle.Source, ctr *atomic.Int64) packhandle.Source {
	return packhandle.Source{
		Open: func() (packhandle.ReadAtCloser, error) {
			ctr.Add(1)
			return src.Open()
		},
		Size: src.Size,
	}
}

// TestCloseIdleDescriptors_ReleasesAndAllowsReuse checks that an idle release
// closes the pack descriptor and that the next cursor reopens it.
func TestCloseIdleDescriptors_ReleasesAndAllowsReuse(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)

	var packOpens atomic.Int64
	src = countingOpen(src, &packOpens)

	h, err := packhandle.NewWithPool(src, hash, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	r, err := h.OpenRandomReader()
	if err != nil {
		t.Fatalf("OpenRandomReader: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close cursor: %v", err)
	}

	packBefore := packOpens.Load()
	if packBefore == 0 {
		t.Fatal("setup did not open the pack")
	}

	if err := h.CloseIdleDescriptors(); err != nil {
		t.Fatalf("CloseIdleDescriptors: %v", err)
	}

	r2, err := h.OpenRandomReader()
	if err != nil {
		t.Fatalf("OpenRandomReader after CloseIdleDescriptors: %v", err)
	}
	var buf [4]byte
	if _, err := r2.ReadAt(buf[:], 0); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt after CloseIdleDescriptors: %v", err)
	}
	_ = r2.Close()

	if packOpens.Load() <= packBefore {
		t.Fatalf(".pack open counter did not advance: before=%d after=%d",
			packBefore, packOpens.Load())
	}
}

// TestCloseIdleDescriptors_AfterCloseIsNoop verifies the no-op
// fast path on closed PackHandles: the closed flag
// short-circuits before touching either SharedFile.
func TestCloseIdleDescriptors_AfterCloseIsNoop(t *testing.T) {
	t.Parallel()
	src, hash := validSourceFromFixture(t)
	h, err := packhandle.NewWithPool(src, hash, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := h.CloseIdleDescriptors(); err != nil {
		t.Fatalf("CloseIdleDescriptors after Close: %v", err)
	}
	if err := h.CloseIdleDescriptors(); err != nil {
		t.Fatalf("CloseIdleDescriptors repeat after Close: %v", err)
	}
}

package packhandle_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
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

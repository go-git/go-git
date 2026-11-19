package packfile_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
)

// stagePackOnDisk copies the fixture's .pack file into a temp
// directory at the canonical objects/pack/pack-<hash>.pack path so
// callers can reopen it through a billy filesystem rooted at the
// returned directory.
func stagePackOnDisk(t *testing.T, f *fixtures.Fixture) (string, plumbing.Hash, string) {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "objects", "pack")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	name := "pack-" + f.PackfileHash + ".pack"
	dst := filepath.Join(dir, name)

	src, err := f.Packfile()
	require.NoError(t, err)
	defer src.Close()

	out, err := os.Create(dst)
	require.NoError(t, err)
	_, err = io.Copy(out, src)
	require.NoError(t, err)
	require.NoError(t, out.Close())

	return root, plumbing.NewHash(f.PackfileHash), filepath.ToSlash(filepath.Join("objects", "pack", name))
}

// TestPackMeta_ParityWithScanner asserts that
// packhandle.parsePackMeta extracts the same trailing pack hash
// as the scanner-driven Seek+ReadFrom path that Packfile.init()
// uses for non-PackHandle constructions. Both paths must agree
// because Packfile.init() now consults PackHandle.Meta() for
// the trailing hash when a PackHandle is set.
func TestPackMeta_ParityWithScanner(t *testing.T) {
	t.Parallel()

	for _, f := range fixtures.Basic().ByTag("packfile") {
		t.Run(f.PackfileHash, func(t *testing.T) {
			t.Parallel()

			root, packHash, relPath := stagePackOnDisk(t, f)
			fs := osfs.New(root)

			// Scanner-driven path: Packfile.init reads the trailing
			// hash when no PackHandle is set.
			p := newPackfile(t, f)
			defer p.Close()
			scannerID, err := p.ID()
			require.NoError(t, err)

			// Meta-driven path: PackHandle.Meta returns the parsed
			// header + footer hash.
			h, err := packhandle.New(packhandle.Sources{
				Pack: packhandle.PathSource(fs, relPath),
			}, packHash)
			require.NoError(t, err)
			defer h.Close()
			meta, err := h.Meta()
			require.NoError(t, err)

			// Both paths must agree on the trailing hash.
			assert.True(t, scannerID.Equal(meta.ID),
				"scanner=%s meta=%s", scannerID, meta.ID)
			// Sanity: the meta's ID also matches the fixture's
			// declared packHash (the value we pinned at New).
			assert.True(t, packHash.Equal(meta.ID),
				"packHash=%s meta=%s", packHash, meta.ID)
		})
	}
}

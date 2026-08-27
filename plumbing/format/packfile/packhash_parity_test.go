package packfile_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
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

func packSource(fs billy.Basic, path string) packhandle.Source {
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

// TestPackHash_ParityWithScanner checks that PackHandle and Scanner read the
// same trailing pack hash.
func TestPackHash_ParityWithScanner(t *testing.T) {
	t.Parallel()

	for _, f := range fixtures.Basic().ByTag("packfile") {
		t.Run(f.PackfileHash, func(t *testing.T) {
			t.Parallel()

			root, packHash, relPath := stagePackOnDisk(t, f)
			fs := osfs.New(root)

			// Scanner-driven path: read the trailing hash directly.
			// Use the hash size derived from the fixture's packHash so
			// the test covers both SHA1 and SHA256 pack fixtures.
			hashSize := packHash.Size()

			pf, err := fs.Open(relPath)
			require.NoError(t, err)
			defer pf.Close()

			scanner := packfile.NewScanner(pf)
			require.True(t, scanner.Scan(), "scanner.Scan: %v", scanner.Error())

			_, err = scanner.Seek(-int64(hashSize), io.SeekEnd)
			require.NoError(t, err)
			var scannerID plumbing.Hash
			scannerID.ResetBySize(hashSize)
			_, err = scannerID.ReadFrom(scanner)
			require.NoError(t, err)

			h, err := packhandle.NewWithPool(packSource(fs, relPath), packHash, nil)
			require.NoError(t, err)
			defer h.Close()
			handleID, err := h.PackHash()
			require.NoError(t, err)

			assert.True(t, scannerID.Equal(handleID),
				"scanner=%s handle=%s", scannerID, handleID)
			assert.True(t, packHash.Equal(handleID),
				"packHash=%s handle=%s", packHash, handleID)
		})
	}
}

package packfile_test

import (
	"io"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
)

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

package transport

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// emptyRepoStorage returns filesystem-backed storage for a fresh repository,
// along with its pack directory. Filesystem storage is required here rather than
// memory storage: the promisor marker is a file next to the pack, so only an
// on-disk storer can record it.
func emptyRepoStorage(t *testing.T) (*filesystem.Storage, string) {
	t.Helper()

	dir := t.TempDir()
	st := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = st.Close() })

	return st, filepath.Join(dir, "objects", "pack")
}

func promisorMarkersIn(t *testing.T, packDir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(packDir, "*.promisor"))
	require.NoError(t, err)
	return m
}

// TestFetchPackMarksFilteredPack pins the behaviour behind the reported bug: a
// fetch that carried a filter must leave the pack marked as coming from a
// promisor remote. The objects the filter excluded are absent from the result,
// and git only accepts that when the pack is marked — unmarked, it reports them
// as broken links and refuses to gc the repository.
func TestFetchPackMarksFilteredPack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     packp.Filter
		wantMarker bool
	}{
		{
			name:       "blob:none marks the pack",
			filter:     packp.FilterBlobNone(),
			wantMarker: true,
		},
		{
			name:       "tree:0 marks the pack",
			filter:     packp.FilterTreeDepth(0),
			wantMarker: true,
		},
		{
			name:       "an unfiltered fetch leaves the pack unmarked",
			filter:     "",
			wantMarker: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			st, packDir := emptyRepoStorage(t)

			pf, err := fixtures.Basic().One().Packfile()
			require.NoError(t, err)

			err = FetchPack(context.Background(), st, capability.List{},
				io.NopCloser(pf), nil, &FetchRequest{Filter: tc.filter})
			require.NoError(t, err)

			markers := promisorMarkersIn(t, packDir)
			if tc.wantMarker {
				require.Len(t, markers, 1,
					"a filtered fetch must mark its pack, or the excluded objects read as corruption")

				// go-git leaves the marker empty. Git fills it with the refs
				// it sought on this path and leaves it empty when repacking,
				// and accepts either, because only presence is consulted.
				fi, err := os.Stat(markers[0])
				require.NoError(t, err)
				assert.Zero(t, fi.Size())

				// The marker has to name the pack it belongs to, otherwise it
				// vouches for nothing.
				pack := markers[0][:len(markers[0])-len(".promisor")] + ".pack"
				_, err = os.Stat(pack)
				assert.NoError(t, err, "marker does not sit beside a pack")
			} else {
				assert.Empty(t, markers)
			}
		})
	}
}

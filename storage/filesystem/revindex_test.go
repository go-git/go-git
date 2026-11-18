package filesystem

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// A config without a pack section must still reach the pack options, so the
// unset cases use one that only sets core.bare.
const revIdxConfigUnset = "[core]\n\tbare = true\n"

func TestNewStorage_PackReadReverseIndex(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config   string
		wantDisk bool
	}{
		"unset": {config: revIdxConfigUnset, wantDisk: true},
		"false": {config: "[pack]\n\treadReverseIndex = false\n", wantDisk: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := osfs.New(t.TempDir())
			require.NoError(t, util.WriteFile(fs, "config", []byte(tc.config), 0o644))
			w, err := dotgit.New(fs).NewObjectPack()
			require.NoError(t, err)
			h := writeFixturePack(t, w)

			revPath := fmt.Sprintf("objects/pack/pack-%s.rev", h)
			require.NoError(t, fs.(billy.Chmod).Chmod(revPath, 0o644))
			require.NoError(t, util.WriteFile(fs, revPath, []byte("not a reverse index"), 0o644))

			sto := NewStorage(fs, cache.NewObjectLRUDefault())
			rev, err := sto.dir.OpenPackRev(h)
			require.NoError(t, err)
			defer func() { require.NoError(t, rev.Close()) }()

			var hdr [4]byte
			_, err = rev.ReadAt(hdr[:], 0)
			require.NoError(t, err)
			if tc.wantDisk {
				assert.Equal(t, "not ", string(hdr[:]), "the .rev on disk should be read")
			} else {
				assert.Equal(t, "RIDX", string(hdr[:]), "the .rev on disk should be ignored")
			}
		})
	}
}

func TestNewStorage_PackWriteReverseIndex(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config  string
		wantRev bool
	}{
		"unset": {config: revIdxConfigUnset, wantRev: true},
		"false": {config: "[pack]\n\twriteReverseIndex = false\n", wantRev: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := osfs.New(t.TempDir())
			require.NoError(t, util.WriteFile(fs, "config", []byte(tc.config), 0o644))
			w, err := NewStorage(fs, cache.NewObjectLRUDefault()).PackfileWriter()
			require.NoError(t, err)
			h := writeFixturePack(t, w)

			_, err = fs.Stat(fmt.Sprintf("objects/pack/pack-%s.rev", h))
			if tc.wantRev {
				assert.NoError(t, err, ".rev file should be written")
			} else {
				assert.ErrorIs(t, err, os.ErrNotExist, ".rev file should not be written")
			}
		})
	}
}

func writeFixturePack(t *testing.T, w io.WriteCloser) plumbing.Hash {
	t.Helper()

	f := fixtures.Basic().One()
	pf, err := f.Packfile()
	require.NoError(t, err)

	_, err = io.Copy(w, pf)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return plumbing.NewHash(f.PackfileHash)
}

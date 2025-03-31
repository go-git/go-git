package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// For additional context: #1159.
func TestIndexEntrySizeUpdatedForNonRegularFiles(t *testing.T) {
	w := osfs.New(t.TempDir(), osfs.WithBoundOS())
	dot, err := w.Chroot(GitDirName)
	require.NoError(t, err)

	s := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	r, err := Init(s, w)
	require.NoError(t, err)
	require.NotNil(t, r)

	wt, err := r.Worktree()
	require.NoError(t, err)
	require.NotNil(t, wt)

	file := "LICENSE"
	f, err := w.OpenFile(file, os.O_CREATE|os.O_WRONLY, 0o666)
	require.NoError(t, err)
	require.NotNil(t, f)

	content := []byte(strings.Repeat("a\n", 1000))
	_, err = f.Write(content)
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	_, err = wt.Add(file)
	require.NoError(t, err)

	_, err = wt.Commit("add file", &CommitOptions{})
	require.NoError(t, err)

	st, err := wt.StatusWithOptions(StatusOptions{Strategy: Preload})
	require.NoError(t, err)
	assert.Equal(t,
		&FileStatus{Worktree: Unmodified, Staging: Unmodified},
		st.File(file))

	// Make the file not regular. The same would apply to a transition
	// from regular file to symlink.
	err = os.Chmod(filepath.Join(w.Root(), file), 0o777)
	require.NoError(t, err)

	f, err = w.OpenFile(file, os.O_APPEND|os.O_RDWR, 0o777)
	require.NoError(t, err)
	require.NotNil(t, f)

	_, err = f.Write([]byte("\n\n"))
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	_, err = wt.Add(file)
	assert.NoError(t, err)

	// go-git's Status diverges from "git status", so this check does not
	// fail, even when the issue is present. As at this point "git status"
	// reports the unstaged file was modified while "git diff" would return
	// empty, as the files are the same but the index has the incorrect file
	// size.
	st, err = wt.StatusWithOptions(StatusOptions{Strategy: Preload})
	assert.NoError(t, err)
	assert.Equal(t,
		&FileStatus{Worktree: Unmodified, Staging: Modified},
		st.File(file))

	idx, err := wt.r.Storer.Index()
	assert.NoError(t, err)
	require.NotNil(t, idx)
	require.Len(t, idx.Entries, 1)

	// Check whether the index was updated with the two new line breaks.
	assert.Equal(t, uint32(len(content)+2), idx.Entries[0].Size)
}

func TestAddEmptyDirectory(t *testing.T) {
	f := fixtures.Basic().One()
	fs := memfs.New()
	r := NewRepositoryWithEmptyWorktree(f)
	w := &Worktree{
		r:          r,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	require.NoError(t, err)

	// write and add foo file in the dir directory
	require.NoError(t, util.WriteFile(fs, "/dir/f1", []byte("foo"), 0644))

	err = w.AddWithOptions(&AddOptions{
		Path:       "/dir/f1",
		SkipStatus: true,
	})
	require.NoError(t, err)

	_, err = w.Commit("commit foo only\n", &CommitOptions{
		Author: defaultSignature(),
	})
	require.NoError(t, err)

	// remove dir directory and add it
	require.NoError(t, util.RemoveAll(fs, "/dir"))

	err = w.AddWithOptions(&AddOptions{
		Path:       "/dir",
		SkipStatus: true,
	})
	require.NoError(t, err)

	_, err = w.Commit("commit remove dir\n", &CommitOptions{
		Author: defaultSignature(),
	})
	require.NoError(t, err)

	// add invalid directory with error
	err = w.AddWithOptions(&AddOptions{
		Path:       "/dir2",
		SkipStatus: true,
	})
	require.Error(t, err)
}

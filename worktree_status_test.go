package git

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// For additional context: #1159.
func TestIndexEntrySizeUpdatedForNonRegularFiles(t *testing.T) {
	t.Parallel()
	w := osfs.New(t.TempDir(), osfs.WithBoundOS())
	dot, err := w.Chroot(GitDirName)
	require.NoError(t, err)

	s := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	r, err := Init(s, WithWorkTree(w))
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

func BenchmarkWorktreeStatus(b *testing.B) {
	repositories := []struct {
		name string
		new  func(b *testing.B) *Repository
		run  func() bool
	}{
		{
			name: "basic-fixture(memfs)",
			run:  func() bool { return true },
			new: func(b *testing.B) *Repository {
				f := fixtures.Basic().One()
				st := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

				r, err := Open(st, memfs.New())
				require.NoError(b, err)

				wt, err := r.Worktree()
				require.NoError(b, err)

				err = wt.Reset(&ResetOptions{Mode: HardReset})
				require.NoError(b, err)

				return r
			},
		},

		{
			name: "basic-fixture(osfs)",
			run:  func() bool { return true },
			new: func(b *testing.B) *Repository {
				temp := b.TempDir()

				f := fixtures.Basic().One()
				dotgit := f.DotGit(fixtures.WithTargetDir(func() string {
					return filepath.Join(temp, GitDirName)
				}))

				st := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

				r, err := Open(st, osfs.New(temp))
				require.NoError(b, err)

				wt, err := r.Worktree()
				require.NoError(b, err)

				err = wt.Reset(&ResetOptions{Mode: HardReset})
				require.NoError(b, err)

				return r
			},
		},
		{
			name: "linux-kernel",
			new: func(b *testing.B) *Repository {
				r, err := PlainOpen("./tests/testdata/repos/linux")
				require.NoError(b, err)

				return r
			},
			run: func() bool {
				_, err := os.Stat("./tests/testdata/repos/linux")
				return err == nil
			},
		},
	}

	b.Run("unmodified(default)", func(b *testing.B) {
		for _, repo := range repositories {
			if !repo.run() {
				continue
			}

			b.Run(repo.name, func(b *testing.B) {
				r := repo.new(b)

				wt, err := r.Worktree()
				require.NoError(b, err)

				for b.Loop() {
					wt.StatusWithOptions(StatusOptions{Strategy: Preload})
				}
			})
		}
	})

	b.Run("ignored-files", func(b *testing.B) {
		addFiles := func(b *testing.B, fs billy.Filesystem) {
			count := 10_000
			base := "ignored"

			require.NoError(b, fs.MkdirAll(base, os.ModeDir|os.ModePerm))

			for i := range count {
				path := path.Join(base, strconv.Itoa(i)+".jar") // *.jar is ignored in basic fixture.

				err := util.WriteFile(fs, path, []byte(path), 0o755)
				require.NoError(b, err)
			}
		}

		for _, repo := range repositories {
			if !strings.Contains(repo.name, "basic-fixture") || !repo.run() {
				// We can only run this in basic repositories.
				continue
			}

			b.Run(repo.name, func(b *testing.B) {
				r := repo.new(b)

				wt, err := r.Worktree()
				require.NoError(b, err)

				addFiles(b, wt.Filesystem)

				for b.Loop() {
					wt.StatusWithOptions(StatusOptions{Strategy: Preload})
				}
			})
		}
	})

	b.Run("all-changed", func(b *testing.B) {
		for _, repo := range repositories {
			if !repo.run() {
				continue
			}

			b.Run(repo.name, func(b *testing.B) {
				r := repo.new(b)

				// Remove all the files from the worktree.
				// TODO: use wt.Remove() when it supports index.
				idx, err := r.Storer.Index()
				require.NoError(b, err)
				newIdx := &index.Index{Version: idx.Version}

				b.Cleanup(func() { require.NoError(b, r.Storer.SetIndex(idx)) })
				require.NoError(b, r.Storer.SetIndex(newIdx))

				wt, err := r.Worktree()
				require.NoError(b, err)

				for b.Loop() {
					wt.StatusWithOptions(StatusOptions{Strategy: Preload})
				}
			})
		}
	})
}

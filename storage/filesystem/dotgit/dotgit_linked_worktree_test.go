package dotgit

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

const linkedWorktreeTestHash = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

// gitStagedPackedRefs is where git writes a new packed-refs before renaming it.
const gitStagedPackedRefs = "packed-refs.new"

// linkedWorktree is a repository with one linked worktree, each on its own
// filesystem: gitdir is .git/worktrees/<name>, commondir is .git.
type linkedWorktree struct {
	gitdir    billy.Filesystem
	commondir billy.Filesystem
	main      *DotGit
	linked    *DotGit
}

// forEachFilesystem runs test on memfs and on the OS filesystem, which differ
// in where they put the files they create.
func forEachFilesystem(t *testing.T, test func(t *testing.T, newFS func() billy.Filesystem)) {
	t.Helper()

	t.Run("memfs", func(t *testing.T) {
		t.Parallel()
		test(t, func() billy.Filesystem { return memfs.New() })
	})
	t.Run("osfs", func(t *testing.T) {
		t.Parallel()
		test(t, func() billy.Filesystem { return osfs.New(t.TempDir()) })
	})
}

func newLinkedWorktree(t *testing.T, newFS func() billy.Filesystem) linkedWorktree {
	t.Helper()

	gitdir, commondir := newFS(), newFS()
	return linkedWorktree{
		gitdir:    gitdir,
		commondir: commondir,
		main:      New(commondir),
		linked:    New(NewRepositoryFilesystem(gitdir, commondir)),
	}
}

func setRefs(t *testing.T, d *DotGit, names ...plumbing.ReferenceName) {
	t.Helper()

	for _, name := range names {
		ref := plumbing.NewHashReference(name, plumbing.NewHash(linkedWorktreeTestHash))
		require.NoError(t, d.SetRef(ref, nil))
	}
}

func requireRefs(t *testing.T, d *DotGit, names ...plumbing.ReferenceName) {
	t.Helper()

	for _, name := range names {
		ref, err := d.Ref(name)
		require.NoError(t, err, name)
		require.Equal(t, linkedWorktreeTestHash, ref.Hash().String(), name)
	}
}

func requireNoRefs(t *testing.T, d *DotGit, names ...plumbing.ReferenceName) {
	t.Helper()

	for _, name := range names {
		_, err := d.Ref(name)
		require.ErrorIs(t, err, plumbing.ErrReferenceNotFound, name)
	}
}

// requireOnePackedRefs checks that git's single packed-refs is in the common
// directory, and that neither directory holds a file staged for it, or any
// packed-refs at all but that one and the files named in others.
func requireOnePackedRefs(t *testing.T, w linkedWorktree, others ...string) {
	t.Helper()

	packedRefsFiles := func(fs billy.Filesystem) (found []string) {
		entries, err := fs.ReadDir(".")
		require.NoError(t, err)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "packed-refs") {
				found = append(found, e.Name())
			}
		}
		return found
	}

	require.Empty(t, packedRefsFiles(w.gitdir), "worktree gitdir")
	require.ElementsMatch(t, append([]string{"packed-refs"}, others...), packedRefsFiles(w.commondir), "common dir")
}

func TestPackRefsFromLinkedWorktree(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		w := newLinkedWorktree(t, newFS)
		setRefs(t, w.linked, "refs/heads/x", "refs/tags/v1")

		require.NoError(t, w.linked.PackRefs())

		requireOnePackedRefs(t, w)
		requireRefs(t, w.main, "refs/heads/x", "refs/tags/v1")
		requireRefs(t, w.linked, "refs/heads/x", "refs/tags/v1")
	})
}

func TestPackRefsFromLinkedWorktreeMergesIntoCommonPackedRefs(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		w := newLinkedWorktree(t, newFS)
		setRefs(t, w.main, "refs/heads/x")
		require.NoError(t, w.main.PackRefs())
		setRefs(t, w.linked, "refs/heads/y")

		require.NoError(t, w.linked.PackRefs())

		requireOnePackedRefs(t, w)
		requireRefs(t, w.main, "refs/heads/x", "refs/heads/y")
	})
}

func TestRemovePackedRefFromLinkedWorktree(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		w := newLinkedWorktree(t, newFS)
		setRefs(t, w.main, "refs/heads/x", "refs/heads/y")
		require.NoError(t, w.main.PackRefs())

		require.NoError(t, w.linked.RemoveRef("refs/heads/x"))

		requireOnePackedRefs(t, w)
		requireNoRefs(t, w.main, "refs/heads/x")
		requireNoRefs(t, w.linked, "refs/heads/x")
		requireRefs(t, w.main, "refs/heads/y")
		requireRefs(t, w.linked, "refs/heads/y")
	})
}

func TestRemoveUnpackedRefFromLinkedWorktreeLeavesNothingStaged(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		w := newLinkedWorktree(t, newFS)
		setRefs(t, w.main, "refs/heads/x")
		require.NoError(t, w.main.PackRefs())
		setRefs(t, w.main, "refs/heads/loose")

		require.NoError(t, w.linked.RemoveRef("refs/heads/loose"))

		requireOnePackedRefs(t, w)
		requireNoRefs(t, w.main, "refs/heads/loose")
		requireRefs(t, w.main, "refs/heads/x")
	})
}

// Each file lives in the common directory or in the worktree's own, and a
// lock file or a staging file goes where the file it locks or replaces does:
// git creates them beside it.
func TestRepositoryFilesystemRoutesFilesAndTheirLockAndStagingFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file     string
		inCommon bool
	}{
		{"packed-refs", true},
		{"refs/heads/x", true},
		{"config", true},
		{"shallow", true},
		{"HEAD", false},
		{"index", false},
		{"config.worktree", false},
		{"logs/HEAD", false},
		{"refs/bisect/bad", false},
		{"refs/rewritten/x", false},
		{"refs/worktree/x", false},
		{"logs/refs/bisect/bad", false},
		{"logs/refs/rewritten/x", false},
		{"logs/refs/worktree/x", false},
		{"refs/bisect-other", true},
		{"logs/refs/heads/main", true},
	}
	type route struct {
		path     string
		inCommon bool
	}
	routes := make([]route, 0, 2*len(tests)+4)
	for _, tc := range tests {
		routes = append(routes, route{tc.file, tc.inCommon}, route{tc.file + ".lock", tc.inCommon})
	}
	// Only packed-refs is staged in a ".new" file. For any other name it is
	// part of the name: a reference may be called refs/bisect.new.
	routes = append(routes,
		route{"packed-refs.new", true},
		route{"packed-refs.0123456789abcdef", true},
		route{"refs/bisect.new", true},
		route{"refs/worktree.new", true},
		route{"logs/refs/rewritten.new", true},
	)
	for _, r := range routes {
		t.Run(r.path, func(t *testing.T) {
			t.Parallel()

			w := newLinkedWorktree(t, func() billy.Filesystem { return memfs.New() })
			fs := NewRepositoryFilesystem(w.gitdir, w.commondir)
			f, err := fs.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
			require.NoError(t, err)
			require.NoError(t, f.Close())

			withFile, other := w.gitdir, w.commondir
			if r.inCommon {
				withFile, other = other, withFile
			}
			_, err = withFile.Stat(r.path)
			require.NoError(t, err)
			_, err = other.Stat(r.path)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// A file named as git names the one it writes a new packed-refs to, left by git
// or by a crashed writer, is neither written through nor removed.
func TestFileNamedLikeGitsStagedPackedRefsIsLeftAlone(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		leftover := []byte(strings.Repeat("not ours\n", 100))
		requireLeftAlone := func(t *testing.T, w linkedWorktree) {
			t.Helper()

			packed, err := util.ReadFile(w.commondir, packedRefsPath)
			require.NoError(t, err)
			require.NotContains(t, string(packed), "not ours")
			got, err := util.ReadFile(w.commondir, gitStagedPackedRefs)
			require.NoError(t, err)
			require.Equal(t, leftover, got)
			requireOnePackedRefs(t, w, gitStagedPackedRefs)
		}

		t.Run("PackRefs", func(t *testing.T) {
			t.Parallel()

			w := newLinkedWorktree(t, newFS)
			require.NoError(t, util.WriteFile(w.commondir, gitStagedPackedRefs, leftover, 0o600))
			setRefs(t, w.linked, "refs/heads/x")

			require.NoError(t, w.linked.PackRefs())

			requireLeftAlone(t, w)
		})

		t.Run("RemoveRef", func(t *testing.T) {
			t.Parallel()

			w := newLinkedWorktree(t, newFS)
			setRefs(t, w.main, "refs/heads/x", "refs/heads/y")
			require.NoError(t, w.main.PackRefs())
			require.NoError(t, util.WriteFile(w.commondir, gitStagedPackedRefs, leftover, 0o600))

			require.NoError(t, w.linked.RemoveRef("refs/heads/x"))

			requireLeftAlone(t, w)
		})
	})
}

// A symlink named as git names the file it writes a new packed-refs to is not
// written through.
func TestStagedPackedRefsSymlinkIsNotFollowed(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		for name, write := range map[string]func(t *testing.T, w linkedWorktree) error{
			"PackRefs": func(t *testing.T, w linkedWorktree) error {
				setRefs(t, w.linked, "refs/heads/x")
				return w.linked.PackRefs()
			},
			"RemoveRef": func(t *testing.T, w linkedWorktree) error {
				setRefs(t, w.main, "refs/heads/x")
				require.NoError(t, w.main.PackRefs())
				return w.linked.RemoveRef("refs/heads/x")
			},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				w := newLinkedWorktree(t, newFS)
				const config = "[core]\n\tbare = true\n"
				require.NoError(t, util.WriteFile(w.commondir, "config", []byte(config), 0o600))
				require.NoError(t, w.commondir.Symlink("config", gitStagedPackedRefs))

				require.NoError(t, write(t, w))

				got, err := util.ReadFile(w.commondir, "config")
				require.NoError(t, err)
				require.Equal(t, config, string(got))
			})
		}
	})
}

// Writers on separate instances, as in separate processes, do not exclude each
// other reliably: the lock on packed-refs can be held on a file already
// replaced, which can lose an update. They must still not fail, or interfere
// with the files each stages, so only the errors are checked.
func TestConcurrentRemoveRefOfPackedRefsDoesNotFail(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir())
	names := make([]plumbing.ReferenceName, 40)
	for i := range names {
		names[i] = plumbing.ReferenceName(fmt.Sprintf("refs/heads/branch-%d", i))
	}
	setRefs(t, New(fs), names...)
	require.NoError(t, New(fs).PackRefs())

	var wg sync.WaitGroup
	errs := make([]error, len(names))
	for i, name := range names {
		wg.Go(func() { errs[i] = New(fs).RemoveRef(name) })
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...))
}

type stagingExistsFS struct {
	billy.Filesystem
	failing  bool
	attempts int
}

func (fs *stagingExistsFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	if fs.failing && strings.HasPrefix(name, stagedPackedRefsPrefix) && flag&os.O_EXCL != 0 {
		fs.attempts++
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrExist}
	}
	return fs.Filesystem.OpenFile(name, flag, perm)
}

// A filesystem that reports every attempt as existing must fail the write
// rather than retry for as long as it holds the lock on packed-refs.
func TestStagingPackedRefsGivesUpWhenEveryNameExists(t *testing.T) {
	t.Parallel()

	for name, write := range map[string]func(d *DotGit) error{
		"PackRefs":  func(d *DotGit) error { return d.PackRefs() },
		"RemoveRef": func(d *DotGit) error { return d.RemoveRef("refs/heads/x") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := &stagingExistsFS{Filesystem: memfs.New()}
			d := New(fs)
			setRefs(t, d, "refs/heads/x", "refs/heads/y")
			if name == "RemoveRef" {
				require.NoError(t, d.PackRefs())
			}
			fs.failing = true

			require.ErrorIs(t, write(d), os.ErrExist)
			require.Equal(t, maxStagedPackedRefsAttempts, fs.attempts)
		})
	}
}

func refNames(t *testing.T, d *DotGit) []plumbing.ReferenceName {
	t.Helper()

	refs, err := d.Refs()
	require.NoError(t, err)
	var names []plumbing.ReferenceName
	for _, ref := range refs {
		if ref.Name() != plumbing.HEAD {
			names = append(names, ref.Name())
		}
	}
	return names
}

func TestRefsListsPerWorktreeRefsOfLinkedWorktree(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		w := newLinkedWorktree(t, newFS)
		setRefs(t, w.linked, "refs/bisect/bad", "refs/rewritten/x", "refs/worktree/y", "refs/heads/z")

		require.ElementsMatch(t,
			[]plumbing.ReferenceName{"refs/bisect/bad", "refs/rewritten/x", "refs/worktree/y", "refs/heads/z"},
			refNames(t, w.linked))
		require.Equal(t, []plumbing.ReferenceName{"refs/heads/z"}, refNames(t, w.main))
	})
}

func TestPackRefsFromLinkedWorktreeLeavesPerWorktreeRefsLoose(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		w := newLinkedWorktree(t, newFS)
		setRefs(t, w.main, "refs/bisect/main-bad", "refs/worktree/main-x", "refs/rewritten/main-x")
		setRefs(t, w.linked, "refs/bisect/linked-bad", "refs/worktree/linked-x", "refs/rewritten/linked-x", "refs/heads/z")

		require.NoError(t, w.linked.PackRefs())

		requireOnePackedRefs(t, w)
		packed, err := util.ReadFile(w.commondir, packedRefsPath)
		require.NoError(t, err)
		require.NotContains(t, string(packed), "refs/bisect")
		require.NotContains(t, string(packed), "refs/worktree")
		require.NotContains(t, string(packed), "refs/rewritten")
		require.ElementsMatch(t,
			[]plumbing.ReferenceName{"refs/bisect/main-bad", "refs/worktree/main-x", "refs/rewritten/main-x", "refs/heads/z"},
			refNames(t, w.main))
		requireRefs(t, w.linked, "refs/bisect/linked-bad", "refs/worktree/linked-x", "refs/rewritten/linked-x")
		requireNoRefs(t, w.main, "refs/bisect/linked-bad", "refs/worktree/linked-x", "refs/rewritten/linked-x")
	})
}

func TestRefNamedLikePerWorktreeDirectoryIsListedAndPacked(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		for _, name := range []plumbing.ReferenceName{"refs/bisect", "refs/rewritten", "refs/worktree"} {
			t.Run(name.String(), func(t *testing.T) {
				t.Parallel()

				d := New(newFS())
				setRefs(t, d, name, "refs/heads/x")

				require.ElementsMatch(t, []plumbing.ReferenceName{name, "refs/heads/x"}, refNames(t, d))
				count, err := d.CountLooseRefs()
				require.NoError(t, err)
				require.Equal(t, 2, count)
				require.NoError(t, d.PackRefs())
				requireRefs(t, d, name, "refs/heads/x")
			})
		}
	})
}

// Git shares the exact name, as is_per_worktree_ref only matches the
// "refs/bisect/" prefix. The router keeps it in the worktree gitdir, so what
// the main worktree sees is deliberately not asserted.
func TestRefNamedLikePerWorktreeDirectoryIsListedFromLinkedWorktree(t *testing.T) {
	t.Parallel()

	forEachFilesystem(t, func(t *testing.T, newFS func() billy.Filesystem) {
		w := newLinkedWorktree(t, newFS)
		setRefs(t, w.linked, "refs/bisect", "refs/heads/x")

		require.ElementsMatch(t, []plumbing.ReferenceName{"refs/bisect", "refs/heads/x"}, refNames(t, w.linked))
	})
}

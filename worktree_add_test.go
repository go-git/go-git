package git

import (
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
)

const siblingFiles = 2

type recordingFS struct {
	billy.Filesystem
	readDirs []string
}

func (f *recordingFS) ReadDir(path string) ([]iofs.DirEntry, error) {
	f.readDirs = append(f.readDirs, path)
	return f.Filesystem.ReadDir(path)
}

// unsortedReadDirFS is a filesystem whose directory listings come back in a
// fixed, deliberately unsorted order. The one on disk returns readdir order,
// which is not guaranteed to be unsorted and on some systems is sorted, so a
// test that depends on the listing being unsorted has to force it rather than
// trust the host.
type unsortedReadDirFS struct {
	billy.Filesystem
	order []string
}

func (f *unsortedReadDirFS) ReadDir(path string) ([]iofs.DirEntry, error) {
	entries, err := f.Filesystem.ReadDir(path)
	if err != nil {
		return nil, err
	}

	byName := make(map[string]iofs.DirEntry, len(entries))
	for _, e := range entries {
		byName[e.Name()] = e
	}

	// Named entries first, in the forced order, then anything else in the
	// listing's own order. A name the listing does not hold is skipped, so the
	// same order serves every directory of the tree.
	out := make([]iofs.DirEntry, 0, len(entries))
	for _, name := range f.order {
		if e, ok := byName[name]; ok {
			out = append(out, e)
			delete(byName, name)
		}
	}
	for _, e := range entries {
		if _, ok := byName[e.Name()]; ok {
			out = append(out, e)
			delete(byName, e.Name())
		}
	}

	return out, nil
}

func siblingWorktree(tb testing.TB, siblings int) (*Worktree, *recordingFS) {
	tb.Helper()

	repo, err := PlainInit(filepath.Join(tb.TempDir(), "repo"), false)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = repo.Close() })

	wt, err := repo.Worktree()
	require.NoError(tb, err)

	for i := range siblings {
		for j := range siblingFiles {
			name := fmt.Sprintf("sib%03d/file%d.txt", i, j)
			require.NoError(tb, util.WriteFile(wt.Filesystem(), name, []byte("sibling\n"), 0o644))
		}
	}

	_, err = wt.Add(".")
	require.NoError(tb, err)
	require.NoError(tb, commitAll(tb, wt, "siblings"))

	require.NoError(tb, util.WriteFile(wt.Filesystem(), "target/inner/deep/added.txt", []byte("added\n"), 0o644))

	rec := &recordingFS{Filesystem: wt.Filesystem()}
	wt.filesystem = newWorktreeFilesystem(rec, defaultProtectNTFS(), defaultProtectHFS())

	return wt, rec
}

func commitAll(tb testing.TB, wt *Worktree, message string) error {
	tb.Helper()

	sig := &object.Signature{
		Name:  "add-scope",
		Email: "add-scope@example.com",
		When:  time.Now(),
	}

	_, err := wt.Commit(message, &CommitOptions{Author: sig, Committer: sig})
	return err
}

func TestAddSubdirectoryWalksOnlyThatSubtree(t *testing.T) {
	t.Parallel()

	const target = "target/inner/deep"

	wt, rec := siblingWorktree(t, 8)

	_, err := wt.Add(target)
	require.NoError(t, err)

	for _, dir := range rec.readDirs {
		if dir == "" || dir == "." || dir == target ||
			strings.HasPrefix(dir, target+"/") || strings.HasPrefix(target, dir+"/") {
			continue
		}

		t.Errorf("Add(%q) listed %q, which is outside the subtree being added", target, dir)
	}

	assert.Contains(t, rec.readDirs, target,
		"Add(%q) should still list the directory it adds", target)
}

func addScopeLayout(tb testing.TB, wt *Worktree) {
	tb.Helper()

	for name, content := range map[string]string{
		"root.txt":                      "root\n",
		"unrelated/other.txt":           "other\n",
		"target/inner/deep/kept.txt":    "kept\n",
		"target/inner/deep/changed.txt": "before\n",
		"target/inner/deep/removed.txt": "removed\n",
	} {
		require.NoError(tb, util.WriteFile(wt.Filesystem(), name, []byte(content), 0o644))
	}

	_, err := wt.Add(".")
	require.NoError(tb, err)
	require.NoError(tb, commitAll(tb, wt, "base"))

	require.NoError(tb, util.WriteFile(wt.Filesystem(), "root.txt", []byte("root changed\n"), 0o644))
	require.NoError(tb, util.WriteFile(wt.Filesystem(), "target/inner/deep/changed.txt", []byte("after\n"), 0o644))
	require.NoError(tb, wt.Filesystem().Remove("target/inner/deep/removed.txt"))
	require.NoError(tb, util.WriteFile(wt.Filesystem(), "target/inner/deep/added.txt", []byte("added\n"), 0o644))
	require.NoError(tb, util.WriteFile(wt.Filesystem(), "target/inner/deep/sub/new.txt", []byte("new\n"), 0o644))
	require.NoError(tb, util.WriteFile(wt.Filesystem(), "target/inner/deep/.gitignore", []byte("*.log\n"), 0o644))
	require.NoError(tb, util.WriteFile(wt.Filesystem(), "target/inner/deep/build.log", []byte("log\n"), 0o644))
}

func TestAddSubdirectoryStagesWhatAnUnscopedAddWould(t *testing.T) {
	t.Parallel()

	const target = "target/inner/deep"

	scopedRepo, scoped, err := layoutRepo(t)
	require.NoError(t, err)
	unscopedRepo, unscoped, err := layoutRepo(t)
	require.NoError(t, err)

	base, err := scopedRepo.Storer.Index()
	require.NoError(t, err)
	baseEntries := make(map[string]plumbing.Hash, len(base.Entries))
	for _, e := range base.Entries {
		baseEntries[e.Name] = e.Hash
	}
	require.NotEmpty(t, baseEntries, "the layout should have staged a base commit")

	_, err = scoped.Add(target)
	require.NoError(t, err)

	_, err = unscoped.Add(".")
	require.NoError(t, err)

	scopedIdx, err := scopedRepo.Storer.Index()
	require.NoError(t, err)
	unscopedIdx, err := unscopedRepo.Storer.Index()
	require.NoError(t, err)

	stagedUnderTarget := 0
	for _, e := range unscopedIdx.Entries {
		if !isPathInDirectory(e.Name, target) {
			continue
		}

		got, err := scopedIdx.Entry(e.Name)
		require.NoError(t, err, "Add(%q) should have staged %q too", target, e.Name)
		assert.Equal(t, e.Hash, got.Hash, "hash of %q", e.Name)
		assert.Equal(t, e.Mode, got.Mode, "mode of %q", e.Name)
		assert.Equal(t, e.Size, got.Size, "size of %q", e.Name)

		// A preexisting tracked file under the target that was MODIFIED must be
		// staged with its new hash. The fixture rewrites
		// target/inner/deep/changed.txt only, so kept.txt legitimately keeps
		// its base hash and the check is scoped to the file that changed: an
		// earlier version of this test skipped every preexisting entry, so a
		// scoped add that handled insertions and deletions but not
		// modifications passed it.
		if baseHash, preexisting := baseEntries[e.Name]; preexisting {
			if e.Name == "target/inner/deep/changed.txt" {
				assert.NotEqual(t, baseHash, got.Hash,
					"Add(%q) did not stage the modification to %q", target, e.Name)
			}
			continue
		}

		stagedUnderTarget++
	}
	assert.Positive(t, stagedUnderTarget, "the layout should have changes under the target")

	for _, e := range scopedIdx.Entries {
		if isPathInDirectory(e.Name, target) {
			continue
		}

		baseHash, tracked := baseEntries[e.Name]
		require.True(t, tracked, "Add(%q) introduced entry %q outside the subtree", target, e.Name)
		assert.Equal(t, baseHash, e.Hash,
			"Add(%q) restaged %q, which is outside the subtree", target, e.Name)
	}

	for name := range baseEntries {
		if isPathInDirectory(name, target) {
			continue
		}

		_, err := scopedIdx.Entry(name)
		assert.NoError(t, err, "Add(%q) dropped %q, which is outside the subtree", target, name)
	}

	_, err = scopedIdx.Entry("target/inner/deep/build.log")
	assert.ErrorIs(t, err, index.ErrEntryNotFound,
		"a file excluded by the .gitignore in the subtree must not be staged")

	_, err = scopedIdx.Entry("target/inner/deep/removed.txt")
	assert.ErrorIs(t, err, index.ErrEntryNotFound,
		"a deleted tracked file in the subtree must be removed from the index")
}

func layoutRepo(t *testing.T) (*Repository, *Worktree, error) {
	t.Helper()

	repo, err := PlainInit(filepath.Join(t.TempDir(), "repo"), false)
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { _ = repo.Close() })

	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, err
	}

	addScopeLayout(t, wt)

	return repo, wt, nil
}

func TestAddDirectoryScopeKeepsAddPathsIntact(t *testing.T) {
	t.Parallel()

	t.Run("root", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)
		require.NoError(t, util.WriteFile(wt.Filesystem(), "root-new.txt", []byte("r\n"), 0o644))

		h, err := wt.Add(".")
		require.NoError(t, err)
		assert.True(t, h.IsZero(), "a directory add returns the zero hash")

		idx, err := wt.r.Storer.Index()
		require.NoError(t, err)
		for _, name := range []string{"root-new.txt", "target/inner/deep/added.txt"} {
			_, err := idx.Entry(name)
			assert.NoError(t, err, "Add(\".\") should stage %q", name)
		}
	})

	t.Run("trailing slash", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)

		_, err := wt.Add("target/inner/deep/")
		require.NoError(t, err)

		idx, err := wt.r.Storer.Index()
		require.NoError(t, err)
		_, err = idx.Entry("target/inner/deep/added.txt")
		assert.NoError(t, err)
	})

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)
		require.NoError(t, util.WriteFile(wt.Filesystem(), "sib001/one.txt", []byte("changed\n"), 0o644))

		h, err := wt.Add("sib001/one.txt")
		require.NoError(t, err)
		assert.False(t, h.IsZero(), "a file add returns the blob hash")

		h, err = wt.Add("sib001/one.txt")
		require.NoError(t, err)
		assert.True(t, h.IsZero(), "re-adding a staged file is a no-op")
	})

	t.Run("absolute path", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)

		abs := filepath.Join(wt.filesystem.Root(), "target", "inner", "deep")
		_, err := wt.Add(abs)
		require.NoError(t, err)

		idx, err := wt.r.Storer.Index()
		require.NoError(t, err)
		_, err = idx.Entry("target/inner/deep/added.txt")
		assert.NoError(t, err, "an absolute directory path is staged relative to the root")
	})

	t.Run("absolute path outside the root", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)

		_, err := wt.Add(filepath.Join(t.TempDir(), "elsewhere"))
		require.Error(t, err)
	})

	t.Run("missing path", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)

		_, err := wt.Add("does/not/exist")
		require.Error(t, err)
	})

	t.Run("skip status", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)
		require.NoError(t, util.WriteFile(wt.Filesystem(), "target/inner/deep/added.txt", []byte("skipped\n"), 0o644))

		require.NoError(t, wt.AddWithOptions(&AddOptions{
			Path:       "target/inner/deep/added.txt",
			SkipStatus: true,
		}))

		idx, err := wt.r.Storer.Index()
		require.NoError(t, err)
		_, err = idx.Entry("target/inner/deep/added.txt")
		assert.NoError(t, err)
	})

	t.Run("ignored directory", func(t *testing.T) {
		t.Parallel()

		wt, _ := siblingWorktree(t, 3)
		require.NoError(t, util.WriteFile(wt.Filesystem(), ".gitignore", []byte("ignored/\n"), 0o644))
		require.NoError(t, util.WriteFile(wt.Filesystem(), "ignored/a.txt", []byte("a\n"), 0o644))

		_, err := wt.Add("ignored")
		require.NoError(t, err)

		idx, err := wt.r.Storer.Index()
		require.NoError(t, err)
		_, err = idx.Entry("ignored/a.txt")
		assert.Error(t, err, "an ignored directory must not be staged")
	})
}

func BenchmarkAddNestedDirectory(b *testing.B) {
	for _, siblings := range []int{4, 64, 256} {
		b.Run(fmt.Sprintf("Siblings_%d", siblings), func(b *testing.B) {
			wt, _ := siblingWorktree(b, siblings)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if _, err := wt.Add("target/inner/deep"); err != nil {
					b.Fatalf("add: %v", err)
				}
			}
		})
	}
}

func TestAddSubdirectoryDoesNotAssumeSortedListing(t *testing.T) {
	t.Parallel()

	repo, err := PlainInit(filepath.Join(t.TempDir(), "repo"), false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })

	wt, err := repo.Worktree()
	require.NoError(t, err)

	dirs := []string{"mmm", "bbb", "qqq", "aaa", "zzz"}
	for _, d := range dirs {
		require.NoError(t, util.WriteFile(wt.Filesystem(), d+"/f.txt", []byte(d), 0o644))
	}

	_, err = wt.Add(".")
	require.NoError(t, err)
	require.NoError(t, commitAll(t, wt, "dirs"))

	// Sorting the listing is filesystem-dependent, and the one on disk sorts
	// nothing: asserting against its order would leave the unsorted case
	// untested on a system that happens to sort. Force the order instead, with
	// the wanted directory last, so a walk that ended the listing at the first
	// entry sorting after the wanted name stages nothing and fails here.
	unsorted := &unsortedReadDirFS{Filesystem: wt.Filesystem(), order: []string{"mmm", "bbb", "qqq", "aaa", "zzz"}}
	wt.filesystem = newWorktreeFilesystem(unsorted, defaultProtectNTFS(), defaultProtectHFS())

	for _, d := range dirs {
		require.NoError(t, util.WriteFile(wt.Filesystem(), d+"/g.txt", []byte("new "+d), 0o644))

		idx, err := repo.Storer.Index()
		require.NoError(t, err)
		idx.Entries = nil
		require.NoError(t, repo.Storer.SetIndex(idx))

		_, err = wt.Add(d)
		require.NoError(t, err)

		idx, err = repo.Storer.Index()
		require.NoError(t, err)

		found := false
		for _, e := range idx.Entries {
			if e.Name == d+"/g.txt" {
				found = true
			}
		}
		assert.True(t, found,
			"Add(%q) staged nothing: the walk stopped at an entry that sorts before the wanted component", d)
	}
}

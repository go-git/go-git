package git

import (
	"fmt"
	iofs "io/fs"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// statusRecordingFS records directory listings and file opens, so tests can
// tell pruning during traversal apart from filtering a full status afterwards.
type statusRecordingFS struct {
	billy.Filesystem

	readDirs []string
	opened   []string
}

// ReadDir returns entries in reverse order, so traversal cannot rely on
// listings being sorted by name.
func (f *statusRecordingFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	f.readDirs = append(f.readDirs, path.Clean(name))

	entries, err := f.Filesystem.ReadDir(name)
	slices.Reverse(entries)

	return entries, err
}

func (f *statusRecordingFS) Open(name string) (billy.File, error) {
	f.opened = append(f.opened, path.Clean(name))
	return f.Filesystem.Open(name)
}

func indexContents(t *testing.T, wt *Worktree) map[string]plumbing.Hash {
	t.Helper()

	idx, err := wt.r.Storer.Index()
	require.NoError(t, err)

	got := make(map[string]plumbing.Hash, len(idx.Entries))
	for _, entry := range idx.Entries {
		got[entry.Name] = entry.Hash
	}

	return got
}

func blobHashes(t *testing.T, contents map[string]string) map[string]plumbing.Hash {
	t.Helper()

	hashes := make(map[string]plumbing.Hash, len(contents))
	for name, content := range contents {
		h := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(content)))
		_, err := h.Write([]byte(content))
		require.NoError(t, err)

		hashes[name] = h.Sum()
	}

	return hashes
}

func TestAddScopeTraversal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		add  func(*Worktree) error
		want map[string]string
	}{
		{
			name: "directory",
			add: func(wt *Worktree) error {
				_, err := wt.Add("docs/generated")
				return err
			},
			want: map[string]string{
				"outside/file.txt":         "staged outside",
				"docs/other/file.txt":      "base",
				"docs/generated/kept.txt":  "changed",
				"docs/generated/added.txt": "new",
			},
		},
		{
			name: "file",
			add: func(wt *Worktree) error {
				_, err := wt.Add("docs/generated/kept.txt")
				return err
			},
			want: map[string]string{
				"outside/file.txt":           "staged outside",
				"docs/other/file.txt":        "base",
				"docs/generated/kept.txt":    "changed",
				"docs/generated/deleted.txt": "base",
			},
		},
		{
			// The glob only matches files on disk, so the deletion stays unstaged.
			name: "glob",
			add: func(wt *Worktree) error {
				return wt.AddGlob("docs/generated/*.txt")
			},
			want: map[string]string{
				"outside/file.txt":           "staged outside",
				"docs/other/file.txt":        "base",
				"docs/generated/kept.txt":    "changed",
				"docs/generated/added.txt":   "new",
				"docs/generated/deleted.txt": "base",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wt := statusScopeWorktree(t)
			for _, name := range []string{
				"outside/file.txt",
				"docs/other/file.txt",
				"docs/generated/kept.txt",
				"docs/generated/deleted.txt",
			} {
				writeStatusScopeFile(t, wt, name, "base")
			}
			commitStatusScopeFiles(t, wt)

			// outside/file.txt differs between HEAD, the index and disk, so a
			// full status walk would have to open it.
			writeStatusScopeFile(t, wt, "outside/file.txt", "staged outside")
			_, err := wt.Add("outside/file.txt")
			require.NoError(t, err)
			writeStatusScopeFile(t, wt, "outside/file.txt", "unstaged outside")

			writeStatusScopeFile(t, wt, "docs/generated/added.txt", "new")
			writeStatusScopeFile(t, wt, "docs/generated/kept.txt", "changed")
			require.NoError(t, wt.Filesystem().Remove("docs/generated/deleted.txt"))

			recorder := &statusRecordingFS{Filesystem: wt.filesystem}
			wt.filesystem = newWorktreeFilesystem(recorder, defaultProtectNTFS(), defaultProtectHFS())

			require.NoError(t, tc.add(wt))

			assert.NotContains(t, recorder.readDirs, "outside")
			assert.NotContains(t, recorder.readDirs, "docs/other")
			assert.NotContains(t, recorder.opened, "outside/file.txt")
			assert.Contains(t, recorder.readDirs, "docs/generated")

			assert.Equal(t, blobHashes(t, tc.want), indexContents(t, wt))
		})
	}
}

func TestAddGlobAbsolutePattern(t *testing.T) {
	t.Parallel()

	// memfs has "/" as its root, so only a real directory distinguishes
	// absolute paths from repository-relative ones.
	repo, err := PlainInit(t.TempDir(), false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	wt, err := repo.Worktree()
	require.NoError(t, err)

	writeStatusScopeFile(t, wt, "docs/a.txt", "a")
	writeStatusScopeFile(t, wt, "docs/b.txt", "b")
	writeStatusScopeFile(t, wt, "other/c.txt", "c")

	require.NoError(t, wt.AddGlob(filepath.Join(wt.filesystem.Root(), "docs", "*.txt")))

	assert.Equal(t, blobHashes(t, map[string]string{
		"docs/a.txt": "a",
		"docs/b.txt": "b",
	}), indexContents(t, wt))

	status, err := wt.Status()
	require.NoError(t, err)
	assert.Equal(t, Status{
		"docs/a.txt":  {Staging: Added, Worktree: Unmodified},
		"docs/b.txt":  {Staging: Added, Worktree: Unmodified},
		"other/c.txt": {Staging: Untracked, Worktree: Untracked},
	}, status)

	outside := filepath.Join(filepath.Dir(wt.filesystem.Root()), "*.txt")
	assert.ErrorContains(t, wt.AddGlob(outside), fmt.Sprintf("path %q is outside the worktree root", outside))
}

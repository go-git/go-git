package git

import (
	iofs "io/fs"
	"path"
	"slices"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// statusRecordingFS records directory listings and file opens so tests can
// distinguish pruning during traversal from filtering a completed status map.
type statusRecordingFS struct {
	billy.Filesystem

	readDirs []string
	opened   []string
}

// ReadDir records the listed directory and reverses the underlying listing
// to exercise traversal with entries that are not in ascending name order.
func (f *statusRecordingFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	f.readDirs = append(f.readDirs, path.Clean(name))

	entries, err := f.Filesystem.ReadDir(name)
	slices.Reverse(entries)

	return entries, err
}

// Open records file access before delegating to the filesystem. Tests use
// this log to detect reads of unrelated files whose contents have changed.
func (f *statusRecordingFS) Open(name string) (billy.File, error) {
	f.opened = append(f.opened, path.Clean(name))
	return f.Filesystem.Open(name)
}

// TestAddScopeTraversal verifies that directory Add, file Add, and AddGlob avoid
// unrelated traversal while staging their selected changes and preserving the rest of the index.
func TestAddScopeTraversal(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"directory", "file", "glob"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			wt := statusScopeWorktree(t)
			wantContents := map[string]string{
				"outside/file.txt":           "base",
				"docs/other/file.txt":        "base",
				"docs/generated/kept.txt":    "base",
				"docs/generated/deleted.txt": "base",
			}
			for name, content := range wantContents {
				writeStatusScopeFile(t, wt, name, content)
			}
			commitStatusScopeFiles(t, wt)

			// Give the unrelated file different HEAD, index, and disk contents.
			writeStatusScopeFile(t, wt, "outside/file.txt", "staged outside")
			_, err := wt.Add("outside/file.txt")
			require.NoError(t, err)

			wantContents["outside/file.txt"] = "staged outside"

			writeStatusScopeFile(t, wt, "outside/file.txt", "unstaged outside")
			writeStatusScopeFile(t, wt, "docs/generated/added.txt", "new")
			writeStatusScopeFile(t, wt, "docs/generated/kept.txt", "changed")
			require.NoError(t, wt.Filesystem().Remove("docs/generated/deleted.txt"))

			recorder := &statusRecordingFS{Filesystem: wt.filesystem}
			wt.filesystem = newWorktreeFilesystem(recorder, defaultProtectNTFS(), defaultProtectHFS())

			switch operation {
			case "directory":
				_, err = wt.Add("docs/generated")
			case "file":
				_, err = wt.Add("docs/generated/kept.txt")
			case "glob":
				err = wt.AddGlob("docs/generated/*.txt")
			}
			require.NoError(t, err)

			// Prune unrelated top-level directories before entering them.
			assert.NotContains(t, recorder.readDirs, "outside")
			// Prune siblings beneath the shared docs ancestor as well.
			assert.NotContains(t, recorder.readDirs, "docs/other")

			// Filtering after a full status walk would still read this changed file.
			assert.NotContains(t, recorder.opened, "outside/file.txt")

			// The selected directory must be reached despite the reversed listing.
			assert.Contains(t, recorder.readDirs, "docs/generated")

			wantContents["docs/generated/kept.txt"] = "changed"

			// Directory Add selects tracked deletions; this glob matches only
			// existing files, and exact-file Add selects only kept.txt.
			if operation == "directory" {
				delete(wantContents, "docs/generated/deleted.txt")
			}

			if operation != "file" {
				wantContents["docs/generated/added.txt"] = "new"
			}

			want := make(map[string]plumbing.Hash)

			// Expected blob hashes come from fixture contents, independently of Add.
			for name, content := range wantContents {
				h := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(content)))
				_, err := h.Write([]byte(content))
				require.NoError(t, err)

				want[name] = h.Sum()
			}

			idx, err := wt.r.Storer.Index()
			require.NoError(t, err)

			got := make(map[string]plumbing.Hash)
			for _, entry := range idx.Entries {
				got[entry.Name] = entry.Hash
			}

			// Compare every staged path and its contents: stage the selected
			// modification/addition/deletion and preserve unrelated staged files.
			assert.Equal(t, want, got)
		})
	}
}

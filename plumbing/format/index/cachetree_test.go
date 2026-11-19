package index

import (
	"crypto"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// repoWithCacheTree commits a nested layout with Git, which leaves a valid
// cache-tree in the index.
func repoWithCacheTree(t *testing.T) string {
	t.Helper()
	if _, err := gitenv.Command("git", "--version").CombinedOutput(); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	for name, body := range map[string]string{
		"top":          "top\n",
		"a/f":          "af\n",
		"a/b/g":        "abg\n",
		"a/b/c/h":      "abch\n",
		"d/e":          "de\n",
		"d.txt":        "dtxt\n",
		"z/y/x/w/deep": "deep\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c")
	return dir
}

func readIndex(t *testing.T, dir string) *Index {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, ".git", "index"))
	require.NoError(t, err)
	defer f.Close()

	idx := &Index{}
	require.NoError(t, NewDecoder(f, crypto.SHA1.New()).Decode(idx))
	return idx
}

func runGitInput(t *testing.T, dir, input string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// fullPaths lists the cache-tree as "dir/:valid" or "dir/:invalid", with each
// directory given by its whole path, so that it can be compared between Git
// and go-git.
func fullPaths(idx *Index) []string {
	if idx.Cache == nil {
		return nil
	}
	es := idx.Cache.Entries
	var out []string
	var walk func(pos int, prefix string) int
	walk = func(pos int, prefix string) int {
		e := es[pos]
		path := prefix
		if e.Path != "" {
			path = prefix + e.Path + "/"
		}
		state := "invalid"
		if e.Entries >= 0 {
			state = "valid"
		}
		out = append(out, path+":"+state)
		next := pos + 1
		for range e.Trees {
			next = walk(next, path)
		}
		return next
	}
	walk(0, "")
	sort.Strings(out)
	return out
}

// InvalidateCache leaves the cache-tree as Git's own index changes do.
func TestInvalidateCacheMatchesGit(t *testing.T) {
	t.Parallel()

	blob := func(t *testing.T, dir string) string {
		return strings.TrimSpace(runGitInput(t, dir, "new\n", "hash-object", "-w", "--stdin"))
	}

	cases := []struct {
		name   string
		path   string
		change func(t *testing.T, dir, path string, idx *Index)
		git    func(t *testing.T, dir, path, blob string)
	}{
		{
			name: "modify a file in a nested directory",
			path: "a/b/g",
			change: func(t *testing.T, dir, path string, idx *Index) {
				e, err := idx.Entry(path)
				require.NoError(t, err)
				idx.InvalidateCache(path)
				e.Hash = plumbing.NewHash(blob(t, dir))
			},
			git: func(t *testing.T, dir, path, b string) {
				runGit(t, dir, "update-index", "--cacheinfo", "100644,"+b+","+path)
			},
		},
		{
			name: "add a file in a new directory",
			path: "a/new/f",
			change: func(t *testing.T, dir, path string, idx *Index) {
				e, err := idx.Add(path)
				require.NoError(t, err)
				e.Mode = 0o100644
				e.Hash = plumbing.NewHash(blob(t, dir))
			},
			git: func(t *testing.T, dir, path, b string) {
				runGit(t, dir, "update-index", "--add", "--cacheinfo", "100644,"+b+","+path)
			},
		},
		{
			name: "remove a file",
			path: "a/b/c/h",
			change: func(t *testing.T, _, path string, idx *Index) {
				_, err := idx.Remove(path)
				require.NoError(t, err)
			},
			git: func(t *testing.T, dir, path, _ string) {
				runGit(t, dir, "update-index", "--force-remove", path)
			},
		},
		{
			name: "replace a directory with a file",
			path: "a/b",
			change: func(t *testing.T, dir, path string, idx *Index) {
				for _, n := range []string{"a/b/g", "a/b/c/h"} {
					_, err := idx.Remove(n)
					require.NoError(t, err)
				}
				e, err := idx.Add(path)
				require.NoError(t, err)
				e.Mode = 0o100644
				e.Hash = plumbing.NewHash(blob(t, dir))
			},
			git: func(t *testing.T, dir, path, b string) {
				runGit(t, dir, "update-index", "--add", "--replace", "--cacheinfo", "100644,"+b+","+path)
			},
		},
		{
			name: "replace a file with a directory",
			path: "top/inner",
			change: func(t *testing.T, dir, path string, idx *Index) {
				_, err := idx.Remove("top")
				require.NoError(t, err)
				e, err := idx.Add(path)
				require.NoError(t, err)
				e.Mode = 0o100644
				e.Hash = plumbing.NewHash(blob(t, dir))
			},
			git: func(t *testing.T, dir, path, b string) {
				runGit(t, dir, "update-index", "--add", "--replace", "--cacheinfo", "100644,"+b+","+path)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			goDir := repoWithCacheTree(t)
			gitDir := t.TempDir()
			require.NoError(t, os.CopyFS(gitDir, os.DirFS(goDir)))

			idx := readIndex(t, goDir)
			tc.change(t, goDir, tc.path, idx)

			b := blob(t, gitDir)
			tc.git(t, gitDir, tc.path, b)
			want := fullPaths(readIndex(t, gitDir))

			assert.Equal(t, want, fullPaths(idx))
		})
	}
}

// PruneCache is what keeps a cache-tree that went stale, because an entry
// was changed in place or the entries were edited directly, from being
// trusted: each change must invalidate the directory and all its parents,
// and nothing else.
func TestPruneCacheInvalidatesWhatChangedUnderneath(t *testing.T) {
	t.Parallel()

	changedBlob := func(t *testing.T, dir string) plumbing.Hash {
		return plumbing.NewHash(strings.TrimSpace(runGitInput(t, dir, "x\n", "hash-object", "-w", "--stdin")))
	}
	byName := func(idx *Index, name string) int {
		for n, e := range idx.Entries {
			if e.Name == name {
				return n
			}
		}
		t.Fatalf("no entry %q", name)
		return -1
	}

	cases := []struct {
		name    string
		mutate  func(t *testing.T, dir string, idx *Index)
		invalid []string
	}{
		{
			name: "hash of a file",
			mutate: func(t *testing.T, dir string, idx *Index) {
				idx.Entries[byName(idx, "a/b/g")].Hash = changedBlob(t, dir)
			},
			invalid: []string{"", "a/", "a/b/"},
		},
		{
			name: "mode of a file",
			mutate: func(_ *testing.T, _ string, idx *Index) {
				idx.Entries[byName(idx, "z/y/x/w/deep")].Mode = 0o100755
			},
			invalid: []string{"", "z/", "z/y/", "z/y/x/", "z/y/x/w/"},
		},
		{
			name: "entry deleted from the slice",
			mutate: func(_ *testing.T, _ string, idx *Index) {
				n := byName(idx, "d/e")
				idx.Entries = append(idx.Entries[:n], idx.Entries[n+1:]...)
			},
			invalid: []string{"", "d/"},
		},
		{
			name: "entry appended to the slice",
			mutate: func(t *testing.T, dir string, idx *Index) {
				idx.Entries = append(idx.Entries, &Entry{Name: "a/b/c/extra", Mode: 0o100644, Hash: changedBlob(t, dir)})
			},
			invalid: []string{"", "a/", "a/b/", "a/b/c/"},
		},
		{
			name: "entry renamed into another directory",
			mutate: func(_ *testing.T, _ string, idx *Index) {
				idx.Entries[byName(idx, "d/e")].Name = "a/e"
			},
			invalid: []string{"", "a/", "d/"},
		},
		{
			name: "directory replaced by a file",
			mutate: func(t *testing.T, dir string, idx *Index) {
				var kept []*Entry
				for _, e := range idx.Entries {
					if !strings.HasPrefix(e.Name, "a/b/") {
						kept = append(kept, e)
					}
				}
				kept = append(kept, &Entry{Name: "a/b", Mode: 0o100644, Hash: changedBlob(t, dir)})
				idx.Entries = kept
			},
			invalid: []string{"", "a/", "a/b/", "a/b/c/"},
		},
		{
			name: "conflict stage",
			mutate: func(_ *testing.T, _ string, idx *Index) {
				idx.Entries[byName(idx, "a/f")].Stage = AncestorMode
			},
			invalid: []string{"", "a/"},
		},
		{
			name: "intent to add",
			mutate: func(_ *testing.T, _ string, idx *Index) {
				idx.Entries[byName(idx, "d/e")].IntentToAdd = true
			},
			invalid: []string{"", "d/"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := repoWithCacheTree(t)
			idx := readIndex(t, dir)
			before := fullPaths(idx)
			tc.mutate(t, dir, idx)

			idx.PruneCache()

			require.NotNil(t, idx.Cache)
			var got []string
			for _, p := range fullPaths(idx) {
				if dirName, ok := strings.CutSuffix(p, ":invalid"); ok {
					got = append(got, dirName)
				}
			}
			assert.ElementsMatch(t, tc.invalid, got, "before: %v", before)
		})
	}
}

func TestPruneCacheKeepsAValidCacheTree(t *testing.T) {
	t.Parallel()

	dir := repoWithCacheTree(t)
	idx := readIndex(t, dir)
	before := fullPaths(idx)

	idx.PruneCache()

	assert.Equal(t, before, fullPaths(idx))
}

func TestPruneCacheDropsACacheTreeThatIsNotATree(t *testing.T) {
	t.Parallel()

	dir := repoWithCacheTree(t)
	for name, entries := range map[string][]TreeEntry{
		"no root":            {{Path: "a", Entries: 1}},
		"missing subtrees":   {{Entries: 7, Trees: 3}, {Path: "a", Entries: 1}},
		"path with slash":    {{Entries: 7, Trees: 1}, {Path: "a/b", Entries: 1}},
		"empty subtree name": {{Entries: 7, Trees: 1}, {Path: "", Entries: 1}},
		"trailing entry":     {{Entries: 7}, {Path: "a", Entries: 1}},
	} {
		idx := readIndex(t, dir)
		idx.Cache = &Tree{Entries: entries}
		idx.PruneCache()
		assert.Nil(t, idx.Cache, name)
	}
}

func TestInvalidateCacheOnAnIndexWithoutOne(t *testing.T) {
	t.Parallel()

	idx := &Index{}
	idx.InvalidateCache("a/b")
	assert.Nil(t, idx.Cache)
}

// FillCache builds the cache-tree Git builds for the same entries.
func TestFillCacheMatchesGits(t *testing.T) {
	t.Parallel()

	dir := repoWithCacheTree(t)
	idx := readIndex(t, dir)
	want := append([]TreeEntry(nil), idx.Cache.Entries...)
	root := plumbing.NewHash(runGit(t, dir, "rev-parse", "HEAD^{tree}"))

	idx.Cache = nil
	require.True(t, idx.FillCache(root))
	assert.Equal(t, want, idx.Cache.Entries)
}

func TestFillCacheRefusesAnotherTree(t *testing.T) {
	t.Parallel()

	dir := repoWithCacheTree(t)
	idx := readIndex(t, dir)
	idx.Cache = nil

	assert.False(t, idx.FillCache(plumbing.NewHash("1111111111111111111111111111111111111111")))
	assert.Nil(t, idx.Cache)

	root := plumbing.NewHash(runGit(t, dir, "rev-parse", "HEAD^{tree}"))
	idx.Entries[0].Stage = AncestorMode
	assert.False(t, idx.FillCache(root))
	assert.Nil(t, idx.Cache)
}

// Git lists the subtrees of a directory by name length first, so that "c"
// comes before "bb".
func TestFillCacheMatchesGitsSubtreeOrder(t *testing.T) {
	t.Parallel()

	if _, err := gitenv.Command("git", "--version").CombinedOutput(); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	for _, name := range []string{"top", "bb/f", "c/f", "ccc/f", "aa/f"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644))
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c")

	idx := readIndex(t, dir)
	want := append([]TreeEntry(nil), idx.Cache.Entries...)
	require.Equal(t, []string{"", "c", "aa", "bb", "ccc"}, treeNames(want))

	idx.Cache = nil
	require.True(t, idx.FillCache(plumbing.NewHash(runGit(t, dir, "rev-parse", "HEAD^{tree}"))))
	assert.Equal(t, want, idx.Cache.Entries)
}

func treeNames(es []TreeEntry) []string {
	names := make([]string, len(es))
	for i, e := range es {
		names[i] = e.Path
	}
	return names
}

// Git rejects this cache-tree with "cache-tree: internal error": the
// negative count of "a" lets the entries balance out at the end of the list.
func TestCacheTreeWithNegativeSubtreeCountIsDropped(t *testing.T) {
	t.Parallel()

	blob := plumbing.NewHash("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")
	idx := &Index{
		Entries: []*Entry{{Name: "a/f", Hash: blob, Mode: filemode.Regular}},
		Cache: &Tree{Entries: []TreeEntry{
			{Path: "", Entries: 1, Trees: 3},
			{Path: "a", Entries: 1, Trees: -1},
			{Path: "b", Entries: 1, Trees: 0},
		}},
	}

	require.NotPanics(t, idx.PruneCache)
	assert.Nil(t, idx.Cache)
}

// The quadratic walk over a cache-tree nested far deeper than the index
// took about 5 GB at this depth.
func TestPruneCacheOfADeepChainStaysCheap(t *testing.T) { //nolint:paralleltest // reads the process-wide allocation count
	// Parallel tests only run once the sequential ones are done, so nothing
	// else allocates while the process-wide count is read.
	const depth = 50000

	entries := make([]TreeEntry, depth)
	for i := range entries {
		entries[i] = TreeEntry{Path: "a", Entries: -1, Trees: 1}
	}
	entries[0].Path = ""
	entries[depth-1].Trees = 0
	idx := &Index{
		Entries: []*Entry{{Name: "a/f", Hash: plumbing.NewHash("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"), Mode: filemode.Regular}},
		Cache:   &Tree{Entries: entries},
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	idx.PruneCache()
	runtime.ReadMemStats(&after)

	require.NotNil(t, idx.Cache)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(32<<20))
}

// Git drops the cache-tree that claims more subtrees than follow, and
// reads the rest of the index.
func TestCacheTreeWithMoreSubtreesThanFollowIsDropped(t *testing.T) {
	t.Parallel()

	blob := plumbing.NewHash("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")
	cache := func() *Tree {
		return &Tree{Entries: []TreeEntry{
			{Path: "", Entries: -1, Trees: 2},
			{Path: "a", Entries: -1, Trees: 0},
		}}
	}

	idx := &Index{Entries: []*Entry{{Name: "a/f", Hash: blob, Mode: filemode.Regular}}, Cache: cache()}
	require.NotPanics(t, idx.PruneCache)
	assert.Nil(t, idx.Cache)

	idx = &Index{Entries: []*Entry{{Name: "a/f", Hash: blob, Mode: filemode.Regular}}, Cache: cache()}
	require.NotPanics(t, func() { idx.InvalidateCache("b/f") })
	require.NotPanics(t, func() { idx.InvalidateCache("a/f") })
}

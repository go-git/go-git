package gitignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// walkScoped walks fs the way a consumer of Scope is meant to: read the root
// patterns once, then derive a child Scope per directory, reading a
// .gitignore only when the listing shows one and the directory is not
// excluded. It returns the ignore verdict for every file it encounters, and
// the set of directories it listed.
func walkScoped(t *testing.T, fs billy.Filesystem) (verdicts, listed map[string]bool) {
	t.Helper()

	base, err := RootPatterns(fs)
	require.NoError(t, err)

	verdicts = map[string]bool{}
	listed = map[string]bool{}

	var walk func(dir []string, scope *Scope)
	walk = func(dir []string, scope *Scope) {
		joined := fs.Join(dir...)
		listed[joined] = true

		entries, err := fs.ReadDir(joined)
		require.NoError(t, err)

		for _, e := range entries {
			if e.Name() == gitDir {
				continue
			}
			child := append(append([]string{}, dir...), e.Name())
			if !e.IsDir() {
				verdicts[fs.Join(child...)] = scope.Match(child, false)
				continue
			}

			// A real consumer knows from its own listing whether the child has
			// an ignore file; here we probe cheaply for the test's purposes.
			var readOwn func() ([]Pattern, error)
			if sub, err := fs.ReadDir(fs.Join(child...)); err == nil {
				for _, s := range sub {
					if s.Name() == gitignoreFile {
						readOwn = func() ([]Pattern, error) { return DirPatterns(fs, child) }
					}
				}
			}

			childScope, err := scope.Descend(child, readOwn)
			require.NoError(t, err)
			walk(child, childScope)
		}
	}

	walk(nil, NewScope(base))
	return verdicts, listed
}

// writeScopeFile creates the slash-separated path, and any parent
// directories, holding content.
func writeScopeFile(t *testing.T, fs billy.Filesystem, path, content string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), os.ModePerm))
	require.NoError(t, util.WriteFile(fs, path, []byte(content), 0o644))
}

// TestScopeExcludedAncestorWins covers the three shapes a flat []Pattern
// cannot get right at once. Reference git reports keep.txt as ignored in all
// three, always attributing it to the rule that excluded the parent
// directory. Verified with `git check-ignore -v`.
func TestScopeExcludedAncestorWins(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, root, nested string
	}{
		{
			// A root negation cannot re-include below an excluded directory,
			// even though a nested file re-excludes it. The flat API gets this
			// right only by accident, by collecting the nested pattern.
			name:   "root negation and nested re-exclude",
			root:   "outer/ignored/\n!outer/ignored/keep.txt\n",
			nested: "keep.txt\n",
		},
		{
			// The same without the nested file. No version of the flat API has
			// ever matched git here.
			name: "root negation alone",
			root: "outer/ignored/\n!outer/ignored/keep.txt\n",
		},
		{
			// A negation inside the excluded directory: the common mistake.
			name:   "nested negation",
			root:   "outer/ignored/\n",
			nested: "!keep.txt\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := memfs.New()
			writeScopeFile(t, fs, ".gitignore", tc.root)
			if tc.nested != "" {
				writeScopeFile(t, fs, "outer/ignored/.gitignore", tc.nested)
			}
			writeScopeFile(t, fs, "outer/ignored/keep.txt", "x\n")

			verdicts, listed := walkScoped(t, fs)

			assert.True(t, verdicts[fs.Join("outer", "ignored", "keep.txt")],
				"an excluded parent directory settles the verdict; git reports keep.txt as ignored")

			// The walk still enters the excluded directory here because this
			// test's walker has no index to tell it otherwise, but the ignore
			// file below it must never influence the result.
			assert.True(t, listed[fs.Join("outer", "ignored")])
		})
	}
}

// TestScopeOverriddenGlobDoesNotReclaimDescendants covers the counterpart to
// TestScopeExcludedAncestorWins: a directory-anchored glob pattern
// ("vendor/g*/") excludes two sibling directories, but a nested .gitignore
// re-includes one of them specifically ("!github.com/"). Once that
// override is resolved, the ancestor glob must not reassert itself against
// files inside the re-included directory — its own anchor ("vendor", "g*")
// is fully consumed at that depth, so it has nothing left to say about
// anything further down. The sibling that was never overridden stays
// excluded via the ordinary sticky Scope.excluded mechanism. Verified with
// `git status --porcelain --ignored`.
func TestScopeOverriddenGlobDoesNotReclaimDescendants(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	writeScopeFile(t, fs, ".gitignore", "vendor/g*/\n")
	writeScopeFile(t, fs, "vendor/.gitignore", "!github.com/\n")
	writeScopeFile(t, fs, "vendor/github.com/file", "x\n")
	writeScopeFile(t, fs, "vendor/gopkg.in/file", "x\n")

	verdicts, _ := walkScoped(t, fs)

	assert.False(t, verdicts[fs.Join("vendor", "github.com", "file")],
		"the nested re-include overrides the ancestor glob for this directory and its contents")
	assert.True(t, verdicts[fs.Join("vendor", "gopkg.in", "file")],
		"the sibling directory was never overridden, so the ancestor glob still excludes it")
}

// TestScopeDoesNotReadIgnoreFilesBelowExcluded verifies that Descend never
// invokes readOwn for an excluded directory, which is what lets a walker skip
// the open entirely.
func TestScopeDoesNotReadIgnoreFilesBelowExcluded(t *testing.T) {
	t.Parallel()

	base := []Pattern{ParsePattern("outer/ignored/", nil)}
	root := NewScope(base)

	outer, err := root.Descend([]string{"outer"}, nil)
	require.NoError(t, err)
	require.False(t, outer.Excluded())

	called := false
	ignored, err := outer.Descend([]string{"outer", "ignored"}, func() ([]Pattern, error) {
		called = true
		return []Pattern{ParsePattern("!keep.txt", []string{"outer", "ignored"})}, nil
	})
	require.NoError(t, err)

	assert.False(t, called, "readOwn must not be called for an excluded directory")
	assert.True(t, ignored.Excluded())
	assert.True(t, ignored.Match([]string{"outer", "ignored", "keep.txt"}, false))

	// Exclusion is sticky: it survives further descent.
	deeper, err := ignored.Descend([]string{"outer", "ignored", "deep"}, func() ([]Pattern, error) {
		called = true
		return nil, nil
	})
	require.NoError(t, err)
	assert.False(t, called)
	assert.True(t, deeper.Excluded())
	assert.True(t, deeper.Match([]string{"outer", "ignored", "deep", "any.txt"}, false))
}

// TestScopeDescendReusesParent checks the allocation-free path: a directory
// that declares no patterns of its own shares its parent's Scope.
func TestScopeDescendReusesParent(t *testing.T) {
	t.Parallel()

	root := NewScope([]Pattern{ParsePattern("*.log", nil)})

	same, err := root.Descend([]string{"src"}, nil)
	require.NoError(t, err)
	assert.Same(t, root, same, "a directory with no ignore file reuses the parent Scope")

	empty, err := root.Descend([]string{"src"}, func() ([]Pattern, error) { return nil, nil })
	require.NoError(t, err)
	assert.Same(t, root, empty, "an ignore file with no usable patterns reuses the parent Scope")
}

// TestScopeDeeperPatternWins checks ordering: a nested rule outranks an
// ancestor one, which is why Descend appends rather than prepends.
func TestScopeDeeperPatternWins(t *testing.T) {
	t.Parallel()

	root := NewScope([]Pattern{ParsePattern("*.log", nil)})
	require.True(t, root.Match([]string{"src", "a.log"}, false))

	src, err := root.Descend([]string{"src"}, func() ([]Pattern, error) {
		return []Pattern{ParsePattern("!a.log", []string{"src"})}, nil
	})
	require.NoError(t, err)

	assert.False(t, src.Match([]string{"src", "a.log"}, false),
		"a negation in a deeper directory outranks an ancestor rule")
	assert.True(t, root.Match([]string{"src", "a.log"}, false),
		"the parent Scope is unchanged: Scope is immutable")
}

// TestScopeZeroValueAndEmptyBase checks the degenerate roots a walker may hit.
func TestScopeZeroValueAndEmptyBase(t *testing.T) {
	t.Parallel()

	for name, s := range map[string]*Scope{
		"zero value": {},
		"nil base":   NewScope(nil),
	} {
		assert.False(t, s.Excluded(), name)
		assert.False(t, s.Match([]string{"anything"}, false), name)
		child, err := s.Descend([]string{"dir"}, nil)
		require.NoError(t, err, name)
		assert.False(t, child.Match([]string{"dir", "f"}, false), name)
	}
}

func TestRootPatternsReadsExcludeThenGitignore(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, fs.MkdirAll(".git/info", os.ModePerm))
	writeScopeFile(t, fs, ".git/info/exclude", "from-exclude\n")
	writeScopeFile(t, fs, ".gitignore", "!from-exclude\n")

	ps, err := RootPatterns(fs)
	require.NoError(t, err)
	require.Len(t, ps, 2)

	assert.False(t, NewScope(ps).Match([]string{"from-exclude"}, false),
		".gitignore is read after .git/info/exclude, so it outranks it")
}

func TestDirPatternsMissingFile(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("empty", os.ModePerm))

	ps, err := DirPatterns(fs, []string{"empty"})
	require.NoError(t, err, "a missing .gitignore is not an error")
	assert.Empty(t, ps)
}

// TestNewScopeCopiesBase pins the immutability the type documents: a caller
// mutating the slice it passed must not be able to change the Scope, or any
// Scope derived from it, after construction.
func TestNewScopeCopiesBase(t *testing.T) {
	t.Parallel()

	base := []Pattern{ParsePattern("build/", nil)}
	root := NewScope(base)
	child, err := root.Descend([]string{"src"}, nil)
	require.NoError(t, err)

	require.True(t, root.Match([]string{"build"}, true))

	// Overwrite the caller's slice in place.
	base[0] = ParsePattern("!build/", nil)

	assert.True(t, root.Match([]string{"build"}, true),
		"the Scope must not observe a later mutation of the caller's slice")
	assert.True(t, child.Match([]string{"build"}, true),
		"nor must a Scope derived from it")
}

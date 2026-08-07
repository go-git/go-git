package gitignore

import (
	iofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadPatternsIgnoresNestedInfoExclude verifies that .git/info/exclude
// is only read at the repository root. Reference git consults
// $GIT_DIR/info/exclude of the repository being walked; a nested
// <dir>/.git/info/exclude belongs to a different repository and must not
// contribute patterns.
func TestReadPatternsIgnoresNestedInfoExclude(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("sub/.git/info", os.ModePerm))
	f, err := fs.Create("sub/.git/info/exclude")
	require.NoError(t, err)
	_, err = f.Write([]byte("ignored.txt\n"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	ps, err := ReadPatterns(fs, nil)
	require.NoError(t, err)
	assert.Empty(t, ps, "nested .git/info/exclude must not be read")
}

// openCounterFS counts Open calls by path.
type openCounterFS struct {
	billy.Filesystem
	opens map[string]int
}

func (fs *openCounterFS) Open(path string) (billy.File, error) {
	fs.opens[path]++
	return fs.Filesystem.Open(path)
}

// readDirCounterFS records the directories ReadPatterns listed, so a test can
// assert on the shape of the walk instead of on wall-clock time.
type readDirCounterFS struct {
	billy.Filesystem
	readDirs map[string]int
}

func (c *readDirCounterFS) ReadDir(path string) ([]iofs.DirEntry, error) {
	c.readDirs[path]++
	return c.Filesystem.ReadDir(path)
}

// writeFile creates the slash-separated path, and any parent directories,
// holding content.
func writeFile(t *testing.T, fs billy.Filesystem, path, content string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), os.ModePerm))
	require.NoError(t, util.WriteFile(fs, path, []byte(content), 0o644))
}

// TestReadPatternsPrunesExcludedDirs verifies that patterns are inherited down
// the walk, so an excluded directory is never descended into however deep below
// the declaring .gitignore it sits. Before patterns were threaded through the
// recursion, a rule could only prune a direct child of its own directory, and
// everything below an ignored subtree was listed anyway.
func TestReadPatternsPrunesExcludedDirs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// ignoreFile is where the rule is declared, rule is its content.
		ignoreFile, rule string
		// pruned is the directory that must never be listed, and deep is a
		// directory below it that must therefore never be listed either.
		pruned, deep string
	}{
		{
			name:       "direct child of declaring dir",
			ignoreFile: ".gitignore",
			rule:       "big/\n",
			pruned:     "big",
			deep:       "big/inner",
		},
		{
			name:       "grandchild of declaring dir",
			ignoreFile: ".gitignore",
			rule:       "outer/ignored/\n",
			pruned:     "outer/ignored",
			deep:       "outer/ignored/deep",
		},
		{
			name:       "unanchored rule matching at depth",
			ignoreFile: ".gitignore",
			rule:       "node_modules/\n",
			pruned:     "a/b/node_modules",
			deep:       "a/b/node_modules/pkg",
		},
		{
			name:       "rule declared in a subdirectory",
			ignoreFile: "outer/.gitignore",
			rule:       "ignored/\n",
			pruned:     "outer/ignored",
			deep:       "outer/ignored/deep",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := memfs.New()
			writeFile(t, base, tc.ignoreFile, tc.rule)
			// A nested ignore file below the pruned directory. Collecting its
			// pattern would prove the walk descended.
			writeFile(t, base, tc.deep+"/.gitignore", "sentinel\n")

			fs := &readDirCounterFS{Filesystem: base, readDirs: map[string]int{}}
			ps, err := ReadPatterns(fs, nil)
			require.NoError(t, err)

			assert.Equal(t, 1, fs.readDirs[""], "the root must be listed exactly once")
			for _, dir := range []string{tc.pruned, tc.deep} {
				assert.Zero(t, fs.readDirs[filepath.FromSlash(dir)],
					"%s is excluded and must never be listed", dir)
			}
			assert.Len(t, ps, 1, "no pattern from below an excluded directory may be collected")

			// And the rule itself still takes effect on the pruned path.
			m := NewMatcher(ps)
			assert.True(t, m.Match(strings.Split(tc.pruned, "/"), true), "%s must be reported as ignored", tc.pruned)
		})
	}
}

// TestReadPatternsExcludedDirCannotReinclude pins the semantics that make
// pruning safe: gitignore(5) says a file cannot be re-included once a parent
// directory of it is excluded, so a negation inside an excluded directory has
// no effect and the directory need not be read.
func TestReadPatternsExcludedDirCannotReinclude(t *testing.T) {
	t.Parallel()
	base := memfs.New()
	writeFile(t, base, ".gitignore", "outer/ignored/\n")
	writeFile(t, base, "outer/ignored/.gitignore", "!keep.txt\n")
	writeFile(t, base, "outer/ignored/keep.txt", "x\n")

	ps, err := ReadPatterns(base, nil)
	require.NoError(t, err)

	// The decision is what matters, so assert it first: reference git reports
	// keep.txt as ignored. Collecting the nested "!keep.txt" gives it the
	// highest priority in the returned set, which flips this to false.
	m := NewMatcher(ps)
	assert.True(t, m.Match([]string{"outer", "ignored"}, true),
		"the excluded directory itself must be ignored")
	assert.True(t, m.Match([]string{"outer", "ignored", "keep.txt"}, false),
		"re-inclusion below an excluded directory is not possible, so keep.txt stays ignored")

	// And the mechanism behind that decision: the negation was never read.
	assert.Len(t, ps, 1, "the negation inside the excluded directory must not be collected")
}

func TestReadPatternsAvoidsBlindOpens(t *testing.T) {
	t.Parallel()

	t.Run("no ignore files", func(t *testing.T) {
		t.Parallel()
		base := memfs.New()
		require.NoError(t, base.MkdirAll("a/b/c", os.ModePerm))
		f, err := base.Create("a/b/c/file.txt")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		fs := &openCounterFS{Filesystem: base, opens: map[string]int{}}
		_, err = ReadPatterns(fs, nil)
		require.NoError(t, err)

		// billy joins with the OS separator; build expectations the same way.
		excludeMarker := fs.Join(gitDir, "info", "exclude")
		for path, n := range fs.opens {
			isIgnoreFile := strings.HasSuffix(path, gitignoreFile) || strings.Contains(path, excludeMarker)
			assert.False(t, isIgnoreFile, "%s was opened %d time(s) but does not exist in the listing", path, n)
		}
	})

	t.Run("present ignore files are read once", func(t *testing.T) {
		t.Parallel()
		base := memfs.New()
		require.NoError(t, base.MkdirAll(".git/info", os.ModePerm))
		require.NoError(t, base.MkdirAll("sub", os.ModePerm))
		for _, p := range []string{".git/info/exclude", ".gitignore", "sub/.gitignore"} {
			f, err := base.Create(p)
			require.NoError(t, err)
			_, err = f.Write([]byte("*.log\n"))
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}

		fs := &openCounterFS{Filesystem: base, opens: map[string]int{}}
		ps, err := ReadPatterns(fs, nil)
		require.NoError(t, err)
		require.Len(t, ps, 3)

		for _, p := range []string{fs.Join(gitDir, "info", "exclude"), fs.Join(gitignoreFile), fs.Join("sub", gitignoreFile)} {
			assert.Equal(t, 1, fs.opens[p], "%s must be opened exactly once", p)
		}
	})
}

// TestReadPatternsMatchesReferenceGit cross-checks the ignore decisions
// produced by ReadPatterns + Matcher against `git ls-files`, over the same
// rule expressed at different placements relative to its target. Pruning an
// excluded directory means ignore files below it are no longer read, so the
// placements have to be shown equivalent rather than merely fast.
//
// Skipped under -short or when no git binary is available, matching
// ConformanceSuite's oracle.
func TestReadPatternsMatchesReferenceGit(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("oracle disabled: git not found: %v", err)
	}

	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{
			name: "rule names a direct child",
			files: map[string]string{
				".gitignore":      "big/\n",
				"big/inner/f.txt": "x\n",
				"keep.txt":        "x\n",
			},
		},
		{
			name: "rule names a grandchild",
			files: map[string]string{
				".gitignore":               "outer/ignored/\n",
				"outer/ignored/deep/f.txt": "x\n",
				"outer/keep.txt":           "x\n",
			},
		},
		{
			name: "rule declared in a subdirectory",
			files: map[string]string{
				"outer/.gitignore":         "ignored/\n",
				"outer/ignored/deep/f.txt": "x\n",
				"outer/keep.txt":           "x\n",
			},
		},
		{
			name: "unanchored rule matching at depth",
			files: map[string]string{
				".gitignore":                 "node_modules/\n",
				"a/b/node_modules/pkg/f.txt": "x\n",
				"a/b/keep.txt":               "x\n",
			},
		},
		{
			// Re-inclusion below an excluded directory is impossible per
			// gitignore(5); the nested negation must have no effect.
			name: "negation inside an excluded directory",
			files: map[string]string{
				".gitignore":               "outer/ignored/\n",
				"outer/ignored/.gitignore": "!keep.txt\n",
				"outer/ignored/keep.txt":   "x\n",
			},
		},
		{
			// The directory itself is not excluded here, only its children,
			// so the walk must still descend into foo/bar.
			name: "children excluded but one re-included",
			files: map[string]string{
				".gitignore":      "foo/*\n!foo/bar\n",
				"foo/bar/baz.txt": "x\n",
				"foo/other.txt":   "x\n",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// CI runs this suite against a locally built Git 2.11.0, which does
			// not honour several re-include patterns. Reuse the conformance
			// suite's exemption rather than maintaining a second list.
			var rules []string
			for p, content := range tc.files {
				if filepath.Base(p) == gitignoreFile {
					rules = append(rules, strings.Fields(content)...)
				}
			}
			if skipOnLegacyGit(rules) {
				t.Skipf("oracle disabled: %s does not support %v", os.Getenv("GIT_VERSION"), rules)
			}

			dir := t.TempDir()
			require.NoError(t, exec.Command("git", "-c", "init.defaultBranch=main", "-C", dir, "init", "-q").Run())

			for p, content := range tc.files {
				abs := filepath.Join(dir, filepath.FromSlash(p))
				require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
				require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
			}

			// Reference: every untracked file git reports as ignored. Nothing
			// is committed, so every file in the tree is untracked.
			out, err := exec.Command("git", "-C", dir, "ls-files", "--others", "--ignored", "--exclude-standard").Output()
			require.NoError(t, err)
			wantIgnored := map[string]bool{}
			for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
				if line != "" {
					wantIgnored[line] = true
				}
			}

			ps, err := ReadPatterns(osfs.New(dir), nil)
			require.NoError(t, err)
			m := NewMatcher(ps)

			// Walk the real tree and ask the matcher about every file.
			require.NoError(t, filepath.WalkDir(dir, func(path string, d iofs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(dir, path)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)
				if rel == "." {
					return nil
				}
				if d.IsDir() {
					if rel == gitDir {
						return iofs.SkipDir
					}
					return nil
				}
				assert.Equal(t, wantIgnored[rel], m.Match(strings.Split(rel, "/"), false),
					"%s: ignore decision must match reference git", rel)
				return nil
			}))
		})
	}
}

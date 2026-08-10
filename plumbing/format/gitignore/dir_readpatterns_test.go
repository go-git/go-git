package gitignore

import (
	"os"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
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

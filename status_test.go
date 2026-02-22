package git

import (
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/storage/memory"
)

func TestStatusReturnsFullPaths(t *testing.T) {
	t.Parallel()
	files := []string{
		filepath.Join("a", "a"),
		filepath.Join("b", "a"),
		filepath.Join("c", "b", "a"),
		filepath.Join("d", "b", "a"),
		filepath.Join("e", "b", "c", "a"),
	}

	expected := map[string]bool{
		"a/a":     true,
		"b/a":     true,
		"c/b/a":   true,
		"d/b/a":   true,
		"e/b/c/a": true,
	}

	r, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	for _, fname := range files {
		file, err := w.Filesystem.Create(fname)
		require.NoError(t, err)

		_, err = file.Write([]byte("foo"))
		require.NoError(t, err)
		file.Close()

		_, err = w.Add(file.Name())
		require.NoError(t, err)
	}

	_, err = w.Commit("foo", &CommitOptions{All: true})
	require.NoError(t, err)

	// Change file contents
	for _, fname := range files {
		file, err := w.Filesystem.Create(fname)
		require.NoError(t, err)

		_, err = file.Write([]byte("fooo"))
		require.NoError(t, err)

		err = file.Close()
		require.NoError(t, err)
	}

	status, err := w.StatusWithOptions(StatusOptions{})
	require.NoError(t, err)

	for file := range status.Iter() {
		yes, ok := expected[file]
		assert.True(t, ok, "unexpected file %q", file)
		assert.True(t, yes, "%q should not be marked as changed", file)
	}

	assert.Equal(t, len(expected), status.Len(), "length mismatch between status and expected files")
}

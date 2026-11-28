package git

import (
	"bytes"
	"crypto/sha1"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

// The TREE extension of an index file is input like any other: a hash in
// it that no longer matches the entries below its directory must not make
// Status report a staged change as clean.
func TestStatusDoesNotTrustAPlantedCacheTreeHash(t *testing.T) {
	t.Parallel()

	dir := repoWithNestedCacheTree(t)
	indexPath := filepath.Join(dir, ".git", "index")
	committed, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	honest := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", ":a/f")))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "f"), []byte("evil\n"), 0o644))
	git(t, dir, "add", "a/f")
	evil := plumbing.NewHash(strings.TrimSpace(git(t, dir, "rev-parse", ":a/f")))

	body := committed[:len(committed)-sha1.Size]
	require.Equal(t, 1, bytes.Count(body, honest.Bytes()))
	planted := bytes.Replace(body, honest.Bytes(), evil.Bytes(), 1)
	sum := sha1.Sum(planted)
	require.NoError(t, os.WriteFile(indexPath, append(planted, sum[:]...), 0o644))
	require.Equal(t, evil.String(), strings.TrimSpace(git(t, dir, "rev-parse", ":a/f")))

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	w, err := r.Worktree()
	require.NoError(t, err)

	status, err := w.Status()
	require.NoError(t, err)
	assert.Equal(t, Modified, status.File("a/f").Staging, "status: %v", status)
}

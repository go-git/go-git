package shallowfetch_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	gogit "github.com/go-git/go-git/v6"
)

// TestDaemonServesFixture proves the harness end to end: a full go-git clone
// through the real git daemon works before any shallow behaviour is touched.
func TestDaemonServesFixture(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	requireGit(t)
	root, _, url := buildServer(t)

	dest := filepath.Join(root, "full-clone")
	_, err := gogit.PlainClone(dest, &gogit.CloneOptions{URL: url})
	require.NoError(t, err)

	require.Equal(t, gitOut(t, filepath.Join(root, "work"), "rev-parse", "HEAD"),
		gitOut(t, dest, "rev-parse", "HEAD"))
}

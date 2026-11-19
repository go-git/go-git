package gitenv_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

func TestRun(t *testing.T) {
	t.Parallel()
	gitenv.RequireAtLeast(t, 1, 0)

	dir := t.TempDir()
	gitenv.Run(t, dir, "init", "--quiet")
	gitenv.Run(t, dir, "commit", "--quiet", "--allow-empty", "-m", "empty")

	assert.Equal(t, "true", gitenv.Run(t, dir, "rev-parse", "--is-inside-work-tree"))
	assert.Equal(t, "1700000000 1700000000", gitenv.Run(t, dir, "log", "-1", "--format=%at %ct"))
}

func TestRequireAtLeastSkipsNewerVersion(t *testing.T) {
	t.Parallel()

	skipped := true
	t.Run("future", func(t *testing.T) {
		gitenv.RequireAtLeast(t, 999, 0)
		skipped = false
	})
	assert.True(t, skipped)
}

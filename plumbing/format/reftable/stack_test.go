package reftable

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openFixtureStack(t *testing.T) *Stack {
	t.Helper()
	f := fixtures.ByTag("reftable").One()
	dotgit, err := f.DotGit()
	require.NoError(t, err)

	reftableDir, err := dotgit.Chroot("reftable")
	require.NoError(t, err)

	stack, err := OpenStack(reftableDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stack.Close() })
	return stack
}

func TestStackRef(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	// HEAD should exist.
	head, err := stack.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, head)
	assert.Equal(t, uint8(refValueSymref), head.ValueType)
	assert.NotEmpty(t, head.Target)

	// The branch HEAD points to should also exist.
	branch, err := stack.Ref(head.Target)
	require.NoError(t, err)
	require.NotNil(t, branch, "branch %s not found", head.Target)
	assert.Equal(t, uint8(refValueVal1), branch.ValueType)
}

func TestStackRefNotFound(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	rec, err := stack.Ref("refs/heads/nonexistent")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestStackIterRefs(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	var names []string
	err := stack.IterRefs(func(rec RefRecord) bool {
		names = append(names, rec.RefName)
		return true
	})
	require.NoError(t, err)

	assert.Contains(t, names, "HEAD")
	assert.Greater(t, len(names), 1, "expected more than just HEAD")
}

func TestStackLogsFor(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	// Find the branch HEAD points to.
	head, err := stack.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, head)

	logs, err := stack.LogsFor(head.Target)
	require.NoError(t, err)

	// Logs may be empty depending on fixture content.
	if len(logs) > 1 {
		// Should be newest first.
		assert.GreaterOrEqual(t, logs[0].UpdateIndex, logs[1].UpdateIndex)
	}
}

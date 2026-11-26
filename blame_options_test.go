package git

import (
	"context"
	"errors"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/diff"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestBlameOptions(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	repo, err := Init(memory.NewStorage(), WithWorkTree(fs))
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	var commit *object.Commit
	for _, content := range []string{"one\nold\n", "one\nnew\n"} {
		require.NoError(t, util.WriteFile(fs, "file", []byte(content), 0o644))
		_, err := wt.Add("file")
		require.NoError(t, err)
		hash, err := wt.Commit("change", &CommitOptions{
			Author: &object.Signature{Name: "Example", Email: "example@example.com"},
		})
		require.NoError(t, err)
		commit, err = repo.CommitObject(hash)
		require.NoError(t, err)
	}
	backend := &blameTestDiffer{}
	options := &BlameOptions{Differ: backend}
	actual, err := BlameWithOptions(t.Context(), commit, "file", options)
	require.NoError(t, err)
	require.Positive(t, backend.calls)
	require.Same(t, backend, options.Differ)
	expected, err := Blame(commit, "file")
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	backend.err = errors.New("backend failed")
	_, err = BlameWithOptions(t.Context(), commit, "file", options)
	require.ErrorIs(t, err, backend.err)
	backend.err = nil
	backend.nilResult = true
	_, err = BlameWithOptions(t.Context(), commit, "file", options)
	require.ErrorContains(t, err, "nil result")
	backend.nilResult = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = BlameWithOptions(ctx, commit, "file", options)
	require.ErrorIs(t, err, context.Canceled)
}

type blameTestDiffer struct {
	calls     int
	err       error
	nilResult bool
}

func (d *blameTestDiffer) Diff(ctx context.Context, oldContent, newContent []byte) (diff.Result, error) {
	d.calls++
	if d.err != nil || d.nilResult {
		return nil, d.err
	}
	return diff.NewDefault().Diff(ctx, oldContent, newContent)
}
